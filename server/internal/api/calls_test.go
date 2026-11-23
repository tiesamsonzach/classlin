package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"classroom-notify/internal/hub"
)

func startAdmins(t *testing.T, token, users, rooms string) (*Server, *httptest.Server) {
	t.Helper()
	s, ts := start(t, token)
	admins, err := ParseAdmins(users, "", "", rooms)
	if err != nil {
		t.Fatal(err)
	}
	ice, err := ParseICEConfig("", "turn:turn.example.com:3478?transport=udp", "", "", "turn-secret-value", "120")
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureCalls(admins, ice, []byte("test-session-secret-key"))
	return s, ts
}

func login(t *testing.T, base, user, pass string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	res, err := client.Post(base+"/api/v1/admin/login", "application/json", strings.NewReader(`{"username":"`+user+`","password":"`+pass+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %d %s", res.StatusCode, raw)
	}
	return client
}

func postJSON(t *testing.T, client *http.Client, url, body string) (int, []byte) {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func TestAdminLoginAndRoomBinding(t *testing.T) {
	_, ts := startAdmins(t, "", "teacher:secret,other:secret2", "teacher:301,other:302")

	code, raw := postJSON(t, nil, ts.URL+"/api/v1/admin/login", `{"username":"teacher","password":"nope"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad password %d %s", code, raw)
	}

	client := login(t, ts.URL, "teacher", "secret")
	res, err := client.Get(ts.URL + "/api/v1/admin/session")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var session struct {
		Authenticated bool     `json:"authenticated"`
		Username      string   `json:"username"`
		AllClassrooms bool     `json:"allClassrooms"`
		Classrooms    []string `json:"classrooms"`
	}
	if err := json.NewDecoder(res.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if !session.Authenticated || session.Username != "teacher" || session.AllClassrooms {
		t.Fatalf("%+v", session)
	}
	if len(session.Classrooms) != 1 || session.Classrooms[0] != "301" {
		t.Fatalf("rooms %+v", session.Classrooms)
	}

	code, raw = postJSON(t, client, ts.URL+"/api/v1/calls", `{"channel":"302","sdp":"v=0"}`)
	if code != http.StatusForbidden || !bytes.Contains(raw, []byte("不能呼叫")) {
		t.Fatalf("unbound classroom %d %s", code, raw)
	}
}

func TestCallAuthBusyAndAutoAnswerGate(t *testing.T) {
	s, ts := startAdmins(t, "s3cret", "teacher:secret,other:secret2", "teacher:301|7K3M-P9WQ,other:301")
	sub, cancel := s.Hub.Subscribe("301")
	defer cancel()

	code, raw := postJSON(t, nil, ts.URL+"/api/v1/calls", `{"channel":"301","sdp":"v=0"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anon call %d %s", code, raw)
	}
	expectNoSignal(t, sub)

	teacher := login(t, ts.URL, "teacher", "secret")
	code, raw = postJSON(t, teacher, ts.URL+"/api/v1/calls", `{"channel":"302","sdp":"v=0"}`)
	if code != http.StatusForbidden {
		t.Fatalf("other room %d %s", code, raw)
	}
	expectNoSignal(t, sub)

	res, err := teacher.Get(ts.URL + "/api/v1/calls?channel=301")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"busy":false`)) {
		t.Fatalf("probe %d %s", res.StatusCode, body)
	}

	code, raw = postJSON(t, teacher, ts.URL+"/api/v1/calls", `{"channel":"301","sdp":"v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\n"}`)
	if code != http.StatusCreated {
		t.Fatalf("start %d %s", code, raw)
	}
	var started struct {
		CallID string `json:"callId"`
	}
	if err := json.Unmarshal(raw, &started); err != nil || started.CallID == "" {
		t.Fatalf("start body %s", raw)
	}
	offer := expectSignal(t, sub)
	if !bytes.Contains(offer, []byte(`"kind":"offer"`)) || !bytes.Contains(offer, []byte(`"from":"admin"`)) || !bytes.Contains(offer, []byte(`"caller":"teacher"`)) {
		t.Fatalf("offer %s", offer)
	}
	if bytes.Contains(offer, []byte("answerKey")) || bytes.Contains(offer, []byte("secret")) {
		t.Fatalf("offer leaked key %s", offer)
	}

	code, raw = postJSON(t, teacher, ts.URL+"/api/v1/calls", `{"channel":"301","sdp":"v=0"}`)
	if code != http.StatusConflict || !bytes.Contains(raw, []byte(`"busy":true`)) {
		t.Fatalf("busy %d %s", code, raw)
	}
	expectNoSignal(t, sub)

	// Knowing the room code is not enough to claim, and a wrong server token is not enough either.
	code, raw = postJSON(t, nil, ts.URL+"/api/v1/calls/verify", `{"channel":"301","token":"wrong","callId":"`+started.CallID+`","claim":true}`)
	if code != http.StatusUnauthorized || bytes.Contains(raw, []byte("answerKey")) {
		t.Fatalf("bad token verify %d %s", code, raw)
	}

	// Peek does not issue the key the classroom needs before it can answer.
	code, raw = postJSON(t, nil, ts.URL+"/api/v1/calls/verify", `{"channel":"301","token":"s3cret","callId":"`+started.CallID+`","claim":false}`)
	if code != http.StatusOK || bytes.Contains(raw, []byte("answerKey")) || !bytes.Contains(raw, []byte(`"caller":"teacher"`)) {
		t.Fatalf("peek %d %s", code, raw)
	}

	code, raw = postJSON(t, nil, ts.URL+"/api/v1/calls/verify", `{"channel":"301","token":"s3cret","callId":"`+started.CallID+`","claim":true}`)
	if code != http.StatusOK {
		t.Fatalf("claim %d %s", code, raw)
	}
	var claim struct {
		OK        bool   `json:"ok"`
		AnswerKey string `json:"answerKey"`
		Caller    string `json:"caller"`
	}
	if err := json.Unmarshal(raw, &claim); err != nil || !claim.OK || claim.AnswerKey == "" || claim.Caller != "teacher" {
		t.Fatalf("claim body %s", raw)
	}
	code, raw = postJSON(t, nil, ts.URL+"/api/v1/calls/verify", `{"channel":"301","token":"s3cret","callId":"`+started.CallID+`","claim":true}`)
	if code != http.StatusForbidden || bytes.Contains(raw, []byte(claim.AnswerKey)) {
		t.Fatalf("second claim %d %s", code, raw)
	}

	// Guessing a call id must not verify, and must not be treated as a reason to open a camera.
	code, raw = postJSON(t, nil, ts.URL+"/api/v1/calls/verify", `{"channel":"301","token":"s3cret","callId":"nope","claim":true}`)
	if code != http.StatusForbidden || !bytes.Contains(raw, []byte(`"ok":false`)) {
		t.Fatalf("unknown call %d %s", code, raw)
	}

	code, raw = postJSON(t, nil, ts.URL+"/api/v1/signal", `{"channel":"301","token":"s3cret","kind":"answer","from":"desktop","callId":"`+started.CallID+`","sdp":"v=0"}`)
	if code != http.StatusForbidden {
		t.Fatalf("answer without key %d %s", code, raw)
	}

	code, raw = postJSON(t, nil, ts.URL+"/api/v1/signal", `{"channel":"301","token":"s3cret","kind":"answer","from":"desktop","callId":"`+started.CallID+`","answerKey":"`+claim.AnswerKey+`","sdp":"v=0\r\n"}`)
	if code != http.StatusAccepted {
		t.Fatalf("answer %d %s", code, raw)
	}
	answer := expectSignal(t, sub)
	if !bytes.Contains(answer, []byte(`"kind":"answer"`)) || !bytes.Contains(answer, []byte(`"from":"desktop"`)) {
		t.Fatalf("answer event %s", answer)
	}

	other := login(t, ts.URL, "other", "secret2")
	code, raw = postJSON(t, other, ts.URL+"/api/v1/signal", `{"channel":"301","kind":"ice","from":"admin","callId":"`+started.CallID+`","candidate":"{}"}`)
	if code != http.StatusForbidden {
		t.Fatalf("foreign ice %d %s", code, raw)
	}
	code, raw = postJSON(t, teacher, ts.URL+"/api/v1/signal", `{"channel":"301","kind":"ice","from":"admin","callId":"`+started.CallID+`","candidate":"{}"}`)
	if code != http.StatusAccepted {
		t.Fatalf("admin ice %d %s", code, raw)
	}
	_ = expectSignal(t, sub)

	code, raw = postJSON(t, nil, ts.URL+"/api/v1/signal", `{"channel":"301","token":"s3cret","kind":"hangup","from":"desktop","callId":"`+started.CallID+`","answerKey":"`+claim.AnswerKey+`"}`)
	if code != http.StatusAccepted {
		t.Fatalf("hangup %d %s", code, raw)
	}
	_ = expectSignal(t, sub)

	code, raw = postJSON(t, teacher, ts.URL+"/api/v1/calls", `{"channel":"301","sdp":"v=0"}`)
	if code != http.StatusCreated {
		t.Fatalf("second session %d %s", code, raw)
	}
	_ = expectSignal(t, sub)
}

func TestRingingExpires(t *testing.T) {
	s, ts := startAdmins(t, "", "teacher:secret", "teacher:*")
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	s.Calls.now = func() time.Time { return now }
	teacher := login(t, ts.URL, "teacher", "secret")
	code, raw := postJSON(t, teacher, ts.URL+"/api/v1/calls", `{"channel":"301","sdp":"v=0"}`)
	if code != http.StatusCreated {
		t.Fatalf("start %d %s", code, raw)
	}
	now = now.Add(46 * time.Second)
	code, raw = postJSON(t, teacher, ts.URL+"/api/v1/calls", `{"channel":"301","sdp":"v=0"}`)
	if code != http.StatusCreated {
		t.Fatalf("after ttl %d %s", code, raw)
	}
}

func TestICERequiresAuthAndHidesSecret(t *testing.T) {
	_, ts := startAdmins(t, "s3cret", "teacher:secret", "teacher:301")
	res, err := http.Get(ts.URL + "/api/v1/ice")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon ice %d %s", res.StatusCode, raw)
	}
	res, err = http.Get(ts.URL + "/api/v1/ice?channel=301")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || bytes.Contains(raw, []byte("turn-secret-value")) {
		t.Fatalf("token ice %d %s", res.StatusCode, raw)
	}

	teacher := login(t, ts.URL, "teacher", "secret")
	res, err = teacher.Get(ts.URL + "/api/v1/ice")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("admin ice %d %s", res.StatusCode, raw)
	}
	if !bytes.Contains(raw, []byte("turn:turn.example.com:3478")) || !bytes.Contains(raw, []byte("stun:stun.l.google.com:19302")) {
		t.Fatalf("servers %s", raw)
	}
	if bytes.Contains(raw, []byte("turn-secret-value")) || !bytes.Contains(raw, []byte(`"credential"`)) {
		t.Fatalf("credential handling %s", raw)
	}
	var payload struct {
		IceServers []struct {
			Username   string `json:"username"`
			Credential string `json:"credential"`
		} `json:"iceServers"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	var turnUser string
	for _, srv := range payload.IceServers {
		if srv.Username != "" {
			turnUser = srv.Username
		}
	}
	if !strings.Contains(turnUser, ":teacher") {
		t.Fatalf("rest username %q in %s", turnUser, raw)
	}
}

func TestTURNRestCredentialShape(t *testing.T) {
	user, cred := mintTURNRest("secret", "教室", time.Unix(1_700_000_000, 0))
	if !strings.HasPrefix(user, "1700000000:") || cred == "" {
		t.Fatalf("%s %s", user, cred)
	}
	cfg, err := ParseICEConfig("stun:example:3478", "", "", "", "", "")
	if err != nil || cfg.STUNURLs[0] != "stun:example:3478" {
		t.Fatal(err, cfg)
	}
	if _, err := ParseICEConfig("", "turn:example:3478", "", "", "", ""); err == nil {
		t.Fatal("open relay should be rejected")
	}
}

func TestClientsDoNotOpenCameraBeforeAuth(t *testing.T) {
	desktop, err := os.ReadFile("../../../desktop/ui/main.js")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := os.ReadFile("../web/static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	policyA, err := os.ReadFile("../../../desktop/ui/call-policy.js")
	if err != nil {
		t.Fatal(err)
	}
	policyB, err := os.ReadFile("../web/static/call-policy.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(policyA, policyB) {
		t.Fatal("call-policy.js copies diverged")
	}
	for _, src := range [][]byte{desktop, admin, policyA} {
		if bytes.Contains(src, []byte("getDisplayMedia")) || bytes.Contains(src, []byte("MediaRecorder")) {
			t.Fatal("screen share or recording was added")
		}
	}
	claimAt := bytes.Index(desktop, []byte("claim: true"))
	mediaAt := bytes.Index(desktop, []byte("getUserMedia"))
	if claimAt < 0 || mediaAt < 0 || claimAt > mediaAt {
		t.Fatalf("desktop opens camera before claim: claim=%d media=%d", claimAt, mediaAt)
	}
	authAt := bytes.Index(admin, []byte("请先登录管理员"))
	mediaAt = bytes.Index(admin, []byte("getUserMedia"))
	if authAt < 0 || mediaAt < 0 || authAt > mediaAt {
		t.Fatalf("admin opens camera before login check: auth=%d media=%d", authAt, mediaAt)
	}
	page, err := os.ReadFile("../web/static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte("管理员")) || !bytes.Contains(page, []byte("登录")) {
		t.Fatal("admin login missing from web page")
	}
}

func expectSignal(t *testing.T, sub <-chan hub.Packet) []byte {
	t.Helper()
	select {
	case pkt := <-sub:
		if pkt.Event != "signal" {
			t.Fatalf("event %s %s", pkt.Event, pkt.Data)
		}
		return pkt.Data
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for signal")
	}
	return nil
}

func expectNoSignal(t *testing.T, sub <-chan hub.Packet) {
	t.Helper()
	select {
	case pkt := <-sub:
		t.Fatalf("unexpected signal %s %s", pkt.Event, pkt.Data)
	case <-time.After(150 * time.Millisecond):
	}
}
