package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	callRingingTTL = 45 * time.Second
	callActiveTTL  = 3 * time.Hour
)

var (
	ErrBusy       = errors.New("busy")
	ErrNoCall     = errors.New("no call")
	ErrCallDenied = errors.New("call denied")
)

type liveCall struct {
	ID        string
	Channel   string
	AdminUser string
	AnswerKey string
	Claimed   bool
	State     string
	Created   time.Time
	Touched   time.Time
}

type CallBook struct {
	mu        sync.Mutex
	byChannel map[string]*liveCall
	now       func() time.Time
}

func NewCallBook() *CallBook {
	return &CallBook{byChannel: map[string]*liveCall{}, now: time.Now}
}

func (b *CallBook) clock() time.Time {
	if b == nil || b.now == nil {
		return time.Now()
	}
	return b.now()
}

func (b *CallBook) live(channel string) *liveCall {
	c := b.byChannel[channel]
	if c == nil {
		return nil
	}
	ttl := callRingingTTL
	if c.State == "active" {
		ttl = callActiveTTL
	}
	if b.clock().Sub(c.Touched) > ttl {
		delete(b.byChannel, channel)
		return nil
	}
	return c
}

// Start opens the single ringing call for a classroom.
// A second start while that call is still live returns ErrBusy and does not replace it.
func (b *CallBook) Start(channel, admin string) (*liveCall, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byChannel == nil {
		b.byChannel = map[string]*liveCall{}
	}
	if cur := b.live(channel); cur != nil {
		return nil, ErrBusy
	}
	now := b.clock()
	c := &liveCall{
		ID:        randomHex(16),
		Channel:   channel,
		AdminUser: admin,
		State:     "ringing",
		Created:   now,
		Touched:   now,
	}
	b.byChannel[channel] = c
	return cloneCall(c), nil
}

func (b *CallBook) Busy(channel string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live(channel) != nil
}

// Peek checks that this ringing call was started by an admin who may still call
// the classroom, and that nobody has claimed it yet. It does not issue an answer
// key and does not open media.
func (b *CallBook) Peek(channel, callID string, allow func(admin string) bool) (*liveCall, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.ringing(channel, callID, allow)
	if err != nil {
		return nil, err
	}
	if c.Claimed {
		return nil, ErrCallDenied
	}
	return cloneCall(c), nil
}

// Claim is the one-time check the classroom performs immediately before opening
// its camera. The answer key is returned only to the first claimant.
func (b *CallBook) Claim(channel, callID string, allow func(admin string) bool) (*liveCall, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.ringing(channel, callID, allow)
	if err != nil {
		return nil, err
	}
	if c.Claimed {
		return nil, ErrCallDenied
	}
	c.Claimed = true
	c.AnswerKey = randomHex(32)
	c.Touched = b.clock()
	return cloneCall(c), nil
}

func (b *CallBook) ringing(channel, callID string, allow func(admin string) bool) (*liveCall, error) {
	c := b.live(channel)
	if c == nil || c.ID != callID {
		return nil, ErrNoCall
	}
	if c.State != "ringing" {
		return nil, ErrBusy
	}
	if allow == nil || !allow(c.AdminUser) {
		delete(b.byChannel, channel)
		return nil, ErrCallDenied
	}
	return c, nil
}

func (b *CallBook) Answer(channel, callID, answerKey string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.live(channel)
	if c == nil || c.ID != callID {
		return ErrNoCall
	}
	if c.State != "ringing" || !c.Claimed || !keyMatch(c.AnswerKey, answerKey) {
		return ErrCallDenied
	}
	now := b.clock()
	c.State = "active"
	c.Touched = now
	return nil
}

func (b *CallBook) AllowICE(channel, callID, from, answerKey, admin string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.live(channel)
	if c == nil || c.ID != callID {
		return ErrNoCall
	}
	switch from {
	case "desktop":
		if !c.Claimed || !keyMatch(c.AnswerKey, answerKey) {
			return ErrCallDenied
		}
	case "admin":
		if admin == "" || admin != c.AdminUser {
			return ErrCallDenied
		}
	default:
		return ErrCallDenied
	}
	c.Touched = b.clock()
	return nil
}

func (b *CallBook) Hangup(channel, callID, from, answerKey, admin string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.live(channel)
	if c == nil || c.ID != callID {
		return ErrNoCall
	}
	switch from {
	case "desktop":
		if !c.Claimed || !keyMatch(c.AnswerKey, answerKey) {
			return ErrCallDenied
		}
	case "admin":
		if admin == "" || admin != c.AdminUser {
			return ErrCallDenied
		}
	default:
		return ErrCallDenied
	}
	delete(b.byChannel, channel)
	return nil
}

func cloneCall(c *liveCall) *liveCall {
	cp := *c
	return &cp
}

func keyMatch(have, got string) bool {
	if have == "" || len(have) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(have), []byte(got)) == 1
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func (s *Server) postCall(w http.ResponseWriter, r *http.Request) {
	if !s.Admins.Configured() {
		writeError(w, http.StatusForbidden, "服务器还没有配置管理员，不能发起视频通话")
		return
	}
	admin, err := s.adminFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录管理员")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var body struct {
		Channel string `json:"channel"`
		SDP     string `json:"sdp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "内容格式不对")
		return
	}
	channel := strings.TrimSpace(firstNonEmpty(body.Channel, channelFrom(r)))
	if !ValidChannel(channel) {
		writeError(w, http.StatusBadRequest, "教室编号请用 1–32 个字符，不要空格")
		return
	}
	if !s.Admins.Allow(admin, channel) {
		writeError(w, http.StatusForbidden, "这个管理员不能呼叫该教室")
		return
	}
	sdp := strings.TrimSpace(body.SDP)
	if sdp == "" || utf8.RuneCountInString(sdp) > 200000 || !strings.HasPrefix(sdp, "v=") {
		writeError(w, http.StatusBadRequest, "通话描述不对")
		return
	}
	if !s.limiter.allow("call:"+admin, 20, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "呼叫太频繁，请稍等")
		return
	}
	call, err := s.Calls.Start(channel, admin)
	if errors.Is(err, ErrBusy) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "教室正在通话", "busy": true})
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "教室正在通话")
		return
	}
	s.Hub.PublishEvent(channel, "signal", mustJSON(map[string]any{
		"kind":   "offer",
		"from":   "admin",
		"callId": call.ID,
		"sdp":    sdp,
		"caller": admin,
	}))
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":     true,
		"callId": call.ID,
		"state":  "ringing",
		"caller": admin,
	})
}

func (s *Server) getCall(w http.ResponseWriter, r *http.Request) {
	if !s.Admins.Configured() {
		writeError(w, http.StatusForbidden, "服务器还没有配置管理员，不能发起视频通话")
		return
	}
	admin, err := s.adminFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录管理员")
		return
	}
	channel := strings.TrimSpace(channelFrom(r))
	if !ValidChannel(channel) {
		writeError(w, http.StatusBadRequest, "教室编号请用 1–32 个字符，不要空格")
		return
	}
	if !s.Admins.Allow(admin, channel) {
		writeError(w, http.StatusForbidden, "这个管理员不能呼叫该教室")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"busy": s.Calls.Busy(channel)})
}

func (s *Server) verifyCall(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Channel string `json:"channel"`
		Token   string `json:"token"`
		CallID  string `json:"callId"`
		Claim   bool   `json:"claim"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "内容格式不对")
		return
	}
	channel, err := s.authorize(firstNonEmpty(body.Channel, channelFrom(r)), firstNonEmpty(body.Token, tokenFrom(r)))
	if err != nil {
		writeAuth(w, err)
		return
	}
	callID := strings.TrimSpace(body.CallID)
	if callID == "" || utf8.RuneCountInString(callID) > 64 {
		writeCallErr(w, ErrNoCall)
		return
	}
	if !s.limiter.allow("verify:"+channel, 60, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "确认来电太频繁")
		return
	}
	allow := func(admin string) bool { return s.Admins.Allow(admin, channel) }
	var call *liveCall
	if body.Claim {
		call, err = s.Calls.Claim(channel, callID, allow)
	} else {
		call, err = s.Calls.Peek(channel, callID, allow)
	}
	if err != nil {
		writeCallErr(w, err)
		return
	}
	out := map[string]any{
		"ok":      true,
		"callId":  call.ID,
		"channel": call.Channel,
		"caller":  call.AdminUser,
		"claimed": call.Claimed,
	}
	if body.Claim {
		out["answerKey"] = call.AnswerKey
	}
	writeJSON(w, http.StatusOK, out)
}

func writeCallErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBusy):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "教室正在通话", "busy": true})
	case errors.Is(err, ErrNoCall), errors.Is(err, ErrCallDenied):
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "无权接听"})
	default:
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "无权接听"})
	}
}
