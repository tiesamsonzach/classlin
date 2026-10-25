package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"classroom-notify/internal/hub"
	"classroom-notify/internal/store"
)

func start(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st, hub.New(), token)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func postMultipart(t *testing.T, rawURL, channel, token, body string, image []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if channel != "" {
		_ = w.WriteField("channel", channel)
	}
	if token != "" {
		_ = w.WriteField("token", token)
	}
	_ = w.WriteField("body", body)
	if image != nil {
		part, err := w.CreateFormFile("image", "a.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL+"/api/v1/messages", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

func TestPhonePageAndInfo(t *testing.T) {
	_, ts := start(t, "pin99")
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !bytes.Contains(body, []byte("进入教室")) || !bytes.Contains(body, []byte("教室编号")) || !bytes.Contains(body, []byte("电脑上显示")) {
		t.Fatalf("page missing copy: %s", body)
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatal(csp)
	}
	infoRes, err := http.Get(ts.URL + "/api/v1/info")
	if err != nil {
		t.Fatal(err)
	}
	defer infoRes.Body.Close()
	var info map[string]any
	if err := json.NewDecoder(infoRes.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info["token_required"] != true {
		t.Fatalf("%v", info)
	}
}

func TestTextImageHistoryAndChineseChannel(t *testing.T) {
	_, ts := start(t, "")
	code, raw := postMultipart(t, ts.URL, "三年二班", "", "请翻开第12页", tinyPNG(t))
	if code != http.StatusCreated {
		t.Fatalf("status %d %s", code, raw)
	}
	var created struct {
		Message struct {
			ID       string `json:"id"`
			HasImage bool   `json:"has_image"`
			Body     string `json:"body"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if !created.Message.HasImage || created.Message.Body != "请翻开第12页" {
		t.Fatalf("%s", raw)
	}
	res, err := http.Get(ts.URL + "/api/v1/messages?channel=" + url.QueryEscape("三年二班") + "&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var page struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != created.Message.ID {
		t.Fatalf("%+v", page)
	}
	bare, err := http.Get(ts.URL + "/api/v1/files/" + created.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	bare.Body.Close()
	if bare.StatusCode != http.StatusBadRequest {
		t.Fatalf("file without channel %d", bare.StatusCode)
	}
	wrong, err := http.Get(ts.URL + "/api/v1/files/" + created.Message.ID + "?channel=301")
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusNotFound {
		t.Fatalf("file other channel %d", wrong.StatusCode)
	}
	img, err := http.Get(ts.URL + "/api/v1/files/" + created.Message.ID + "?channel=" + url.QueryEscape("三年二班"))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Body.Close()
	if img.StatusCode != http.StatusOK || !strings.HasPrefix(img.Header.Get("Content-Type"), "image/png") {
		t.Fatalf("image %d %s", img.StatusCode, img.Header.Get("Content-Type"))
	}
	other, err := http.Get(ts.URL + "/api/v1/messages?channel=301")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Body.Close()
	var empty struct {
		Messages []any `json:"messages"`
	}
	if err := json.NewDecoder(other.Body).Decode(&empty); err != nil {
		t.Fatal(err)
	}
	if len(empty.Messages) != 0 {
		t.Fatal("channel leak")
	}
}

func TestTokenAndCookie(t *testing.T) {
	_, ts := start(t, "s3cret")
	code, raw := postMultipart(t, ts.URL, "301", "", "nope", nil)
	if code != http.StatusUnauthorized || !bytes.Contains(raw, []byte("口令不正确")) {
		t.Fatalf("%d %s", code, raw)
	}
	code, raw = postMultipart(t, ts.URL, "301", "s3cret", "<b>你好</b>", nil)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte(`\u003c`)) {
		t.Fatalf("expected html escape %s", raw)
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pair", strings.NewReader(`{"channel":"301","token":"s3cret"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	cookies := res.Cookies()
	var id string
	json.Unmarshal(raw, &struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}{})
	var created struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	id = created.Message.ID
	imgReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/files/"+id+"?channel=301", nil)
	noAuth, err := http.DefaultClient.Do(imgReq)
	if err != nil {
		t.Fatal(err)
	}
	noAuth.Body.Close()
	if noAuth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("file without token %d", noAuth.StatusCode)
	}
	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/messages?limit=5", nil)
	for _, c := range cookies {
		listReq.AddCookie(c)
	}
	listRes, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	if listRes.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(listRes.Body)
		t.Fatalf("cookie list %d %s", listRes.StatusCode, b)
	}
}

func TestRejectsEmptyBadImageAndOversized(t *testing.T) {
	s, ts := start(t, "")
	code, raw := postMultipart(t, ts.URL, "301", "", "   ", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("empty %d %s", code, raw)
	}
	code, raw = postMultipart(t, ts.URL, "301", "", "", []byte("not an image!!!!"))
	if code != http.StatusBadRequest || !bytes.Contains(raw, []byte("格式")) {
		t.Fatalf("bad image %d %s", code, raw)
	}
	code, raw = postMultipart(t, ts.URL, "a/b", "", "hi", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("channel %d %s", code, raw)
	}
	s.MaxImage = 8
	code, raw = postMultipart(t, ts.URL, "301", "", "", append(tinyPNG(t), bytes.Repeat([]byte{0}, 32)...))
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("size %d %s", code, raw)
	}
	s.RatePerMin = 1
	code, _ = postMultipart(t, ts.URL, "301", "", "一次", nil)
	if code != http.StatusCreated {
		t.Fatal(code)
	}
	code, raw = postMultipart(t, ts.URL, "301", "", "两次", nil)
	if code != http.StatusTooManyRequests {
		t.Fatalf("rate %d %s", code, raw)
	}
}

func TestJSONAndHeaderAuth(t *testing.T) {
	_, ts := start(t, "tok")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/messages", strings.NewReader(`{"body":"黑板这道题"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Channel", "301")
	req.Header.Set("X-Token", "tok")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%d %s", res.StatusCode, b)
	}
}

func TestStreamDeliversUnderTwoSecondsAndIsolates(t *testing.T) {
	_, ts := start(t, "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	open := func(channel string) *bufio.Reader {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/stream?channel="+channel+"&token=tok", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatal(res.Status)
		}
		rd := bufio.NewReader(res.Body)
		if _, err := rd.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		return rd
	}
	room := open("301")
	other := open("302")

	startAt := time.Now()
	code, raw := postMultipart(t, ts.URL, "301", "tok", "请翻开第12页", nil)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	deadline := time.Now().Add(2 * time.Second)
	got := ""
	for time.Now().Before(deadline) {
		line, err := room.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "翻开") {
			got = line
			break
		}
	}
	if got == "" || time.Since(startAt) > 2*time.Second {
		t.Fatalf("slow or missing %s", time.Since(startAt))
	}

	silence := make(chan struct{})
	go func() {
		for {
			line, err := other.ReadString('\n')
			if err != nil || strings.HasPrefix(line, "data:") {
				close(silence)
				return
			}
		}
	}()
	select {
	case <-silence:
		t.Fatal("other channel received data")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestGetMessageMatchesChannel(t *testing.T) {
	_, ts := start(t, "")
	code, raw := postMultipart(t, ts.URL, "301", "", "hello", nil)
	if code != http.StatusCreated {
		t.Fatal(string(raw))
	}
	var created struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(ts.URL + "/api/v1/messages/" + created.Message.ID + "?channel=302")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatal(res.Status)
	}
	ok, err := http.Get(ts.URL + "/api/v1/messages/" + created.Message.ID + "?channel=301")
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatal(ok.Status)
	}
}

func TestHealth(t *testing.T) {
	_, ts := start(t, "")
	res, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !bytes.Contains(body, []byte(`"ok":true`)) {
		t.Fatal(string(body))
	}
}
