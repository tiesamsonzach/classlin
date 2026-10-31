package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"classroom-notify/internal/hub"
	"classroom-notify/internal/store"
	"classroom-notify/internal/web"
)

const maxBodyRunes = 2000

type Server struct {
	Store    *store.Store
	Hub      *hub.Hub
	Token    string
	MaxImage int64

	RatePerMin int
	limiter    rateLimiter
}

func New(st *store.Store, h *hub.Hub, token string) *Server {
	return &Server{
		Store:      st,
		Hub:        h,
		Token:      token,
		MaxImage:   12 << 20,
		RatePerMin: 30,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/v1/info", s.info)
	mux.HandleFunc("POST /api/v1/pair", s.pair)
	mux.HandleFunc("POST /api/v1/messages", s.postMessage)
	mux.HandleFunc("GET /api/v1/messages", s.listMessages)
	mux.HandleFunc("GET /api/v1/messages/{id}", s.getMessage)
	mux.HandleFunc("GET /api/v1/files/{id}", s.getFile)
	mux.HandleFunc("GET /api/v1/stream", s.stream)
	mux.HandleFunc("POST /api/v1/signal", s.postSignal)
	mux.Handle("GET /", web.Static())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"token_required":  s.Token != "",
		"max_image_bytes": s.MaxImage,
		"max_body_runes":  maxBodyRunes,
		"version":         "1.0.0",
	})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Channel string `json:"channel"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if tooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "请求太大")
			return
		}
		writeError(w, http.StatusBadRequest, "请填写教室编号")
		return
	}
	channel, err := s.authorize(body.Channel, body.Token)
	if err != nil {
		writeAuth(w, err)
		return
	}
	secure := requestHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     "cn_channel",
		Value:    url.QueryEscape(channel),
		Path:     "/",
		MaxAge:   86400 * 365,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	if s.Token == "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "cn_token",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   secure,
		})
	} else {
		http.SetCookie(w, &http.Cookie{
			Name:     "cn_token",
			Value:    url.QueryEscape(s.Token),
			Path:     "/",
			MaxAge:   86400 * 365,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   secure,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"channel":        channel,
		"token_required": s.Token != "",
	})
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.MaxImage+(256<<10))
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "application/json"):
		s.postJSON(w, r)
	case strings.HasPrefix(ct, "multipart/form-data"):
		s.postMultipart(w, r)
	default:
		writeError(w, http.StatusBadRequest, "请用表单或 JSON 提交")
	}
}

func (s *Server) postJSON(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
		Token   string `json:"token"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if tooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, s.tooBig())
			return
		}
		writeError(w, http.StatusBadRequest, "内容格式不对")
		return
	}
	channel := firstNonEmpty(body.Channel, channelFrom(r))
	token := firstNonEmpty(body.Token, tokenFrom(r))
	s.finishPost(w, r, channel, token, body.Body, nil)
}

func (s *Server) postMultipart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		if tooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, s.tooBig())
			return
		}
		writeError(w, http.StatusBadRequest, "表单格式不对")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	channel := firstNonEmpty(formValue(r, "channel"), channelFrom(r))
	token := firstNonEmpty(formValue(r, "token"), tokenFrom(r))
	text := r.FormValue("body")
	var image []byte
	file, _, err := r.FormFile("image")
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		writeError(w, http.StatusBadRequest, "照片读取失败")
		return
	}
	if file != nil {
		defer file.Close()
		image, err = io.ReadAll(io.LimitReader(file, s.MaxImage+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "照片读取失败")
			return
		}
		if int64(len(image)) > s.MaxImage {
			writeError(w, http.StatusRequestEntityTooLarge, s.tooBig())
			return
		}
		if len(image) == 0 {
			image = nil
		}
	}
	s.finishPost(w, r, channel, token, text, image)
}

func (s *Server) finishPost(w http.ResponseWriter, r *http.Request, channel, token, text string, image []byte) {
	channel, err := s.authorize(channel, token)
	if err != nil {
		writeAuth(w, err)
		return
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxBodyRunes {
		writeError(w, http.StatusBadRequest, "文字太长，最多 2000 字")
		return
	}
	mime := ""
	if len(image) > 0 {
		var ok bool
		mime, ok = sniffImage(image)
		if !ok {
			writeError(w, http.StatusBadRequest, "照片格式不支持，请换 JPG、PNG、GIF 或 WebP")
			return
		}
	}
	if text == "" && mime == "" {
		writeError(w, http.StatusBadRequest, "请填写文字或选择照片")
		return
	}
	if !s.limiter.allow(channel, s.RatePerMin, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "发送太频繁，请稍等几秒")
		return
	}
	msg, err := s.Store.Add(channel, text, mime, image)
	if err != nil {
		log.Printf("保存失败 教室=%s err=%v", channel, err)
		writeError(w, http.StatusInternalServerError, "服务器保存失败")
		return
	}
	payload := mustJSON(s.messageJSON(msg))
	s.Hub.Publish(channel, payload)
	log.Printf("新消息 教室=%s 编号=%s 有照片=%t", channel, msg.ID, mime != "")
	writeJSON(w, http.StatusCreated, map[string]any{"message": s.messageJSON(msg)})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	channel, err := s.authorize(channelFrom(r), tokenFrom(r))
	if err != nil {
		writeAuth(w, err)
		return
	}
	limit := 30
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "参数不对")
			return
		}
		limit = n
	}
	var before int64
	if q := r.URL.Query().Get("before"); q != "" {
		n, err := strconv.ParseInt(q, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "参数不对")
			return
		}
		before = n
	}
	page, err := s.Store.List(channel, before, limit)
	if err != nil {
		log.Printf("读取历史失败 教室=%s err=%v", channel, err)
		writeError(w, http.StatusInternalServerError, "服务器读取失败")
		return
	}
	msgs := make([]messageJSON, 0, len(page.Messages))
	for _, m := range page.Messages {
		msgs = append(msgs, s.messageJSON(m))
	}
	var next *int64
	if page.NextBefore > 0 {
		next = &page.NextBefore
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages":    msgs,
		"next_before": next,
	})
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	channel, err := s.authorize(channelFrom(r), tokenFrom(r))
	if err != nil {
		writeAuth(w, err)
		return
	}
	msg, err := s.Store.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && msg.Channel != channel) {
		writeError(w, http.StatusNotFound, "找不到这条消息")
		return
	}
	if err != nil {
		log.Printf("读取消息失败 err=%v", err)
		writeError(w, http.StatusInternalServerError, "服务器读取失败")
		return
	}
	writeJSON(w, http.StatusOK, s.messageJSON(msg))
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	channel, err := s.authorize(channelFrom(r), tokenFrom(r))
	if err != nil {
		writeAuth(w, err)
		return
	}
	id := r.PathValue("id")
	msg, err := s.Store.Get(id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && (msg.ImageMime == "" || msg.Channel != channel)) {
		writeError(w, http.StatusNotFound, "找不到这张照片")
		return
	}
	if err != nil {
		log.Printf("读取照片失败 err=%v", err)
		writeError(w, http.StatusInternalServerError, "服务器读取失败")
		return
	}
	f, err := s.Store.OpenImage(id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "照片文件丢失")
		return
	}
	if err != nil {
		log.Printf("打开照片失败 err=%v", err)
		writeError(w, http.StatusInternalServerError, "服务器读取失败")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器读取失败")
		return
	}
	w.Header().Set("Content-Type", msg.ImageMime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, "", stat.ModTime(), f)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	channel, err := s.authorize(channelFrom(r), tokenFrom(r))
	if err != nil {
		writeAuth(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "服务器不支持推送")
		return
	}
	ch, cancel := s.Hub.Subscribe(channel)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// Pad the first bytes so reverse proxies that buffer small writes still flush the stream.
	fmt.Fprintf(w, "retry: 2000\n: %s\n\n", strings.Repeat(" ", 2048))
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case pkt, ok := <-ch:
			if !ok {
				return
			}
			event := pkt.Event
			if event == "" {
				event = "message"
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n: %s\n\n", event, pkt.Data, strings.Repeat(" ", 2048)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) postSignal(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var body struct {
		Channel   string `json:"channel"`
		Token     string `json:"token"`
		Kind      string `json:"kind"`
		From      string `json:"from"`
		CallID    string `json:"callId"`
		SDP       string `json:"sdp"`
		Candidate string `json:"candidate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "内容格式不对")
		return
	}
	channel := firstNonEmpty(body.Channel, channelFrom(r))
	token := firstNonEmpty(body.Token, tokenFrom(r))
	channel, err := s.authorize(channel, token)
	if err != nil {
		writeAuth(w, err)
		return
	}
	kind := strings.TrimSpace(body.Kind)
	from := strings.TrimSpace(body.From)
	switch kind {
	case "offer", "answer", "ice", "hangup", "ringing":
	default:
		writeError(w, http.StatusBadRequest, "不支持的信令类型")
		return
	}
	switch from {
	case "desktop", "phone":
	default:
		writeError(w, http.StatusBadRequest, "from 只能是 desktop 或 phone")
		return
	}
	callID := strings.TrimSpace(body.CallID)
	if utf8.RuneCountInString(callID) > 64 {
		writeError(w, http.StatusBadRequest, "callId 太长")
		return
	}
	if utf8.RuneCountInString(body.SDP) > 200000 || utf8.RuneCountInString(body.Candidate) > 8000 {
		writeError(w, http.StatusBadRequest, "信令内容太大")
		return
	}
	out := map[string]any{
		"kind":   kind,
		"from":   from,
		"callId": callID,
	}
	if body.SDP != "" {
		out["sdp"] = body.SDP
	}
	if body.Candidate != "" {
		out["candidate"] = body.Candidate
	}
	payload := mustJSON(out)
	s.Hub.PublishEvent(channel, "signal", payload)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) authorize(channel, token string) (string, error) {
	channel = strings.TrimSpace(channel)
	if !ValidChannel(channel) {
		return "", ErrBadChannel
	}
	if err := s.checkToken(token); err != nil {
		return "", err
	}
	return channel, nil
}

func (s *Server) checkToken(token string) error {
	if s.Token == "" {
		return nil
	}
	if !ValidToken(token) {
		return ErrBadToken
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Server) tooBig() string {
	mb := s.MaxImage / (1 << 20)
	if mb < 1 {
		mb = 1
	}
	return fmt.Sprintf("照片太大，最大 %dMB", mb)
}

type messageJSON struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Body      string `json:"body"`
	HasImage  bool   `json:"has_image"`
	ImageURL  string `json:"image_url,omitempty"`
	ImageMime string `json:"image_mime,omitempty"`
	CreatedAt string `json:"created_at"`
	CreatedMs int64  `json:"created_ms"`
}

func (s *Server) messageJSON(m store.Message) messageJSON {
	out := messageJSON{
		ID:        m.ID,
		Channel:   m.Channel,
		Body:      m.Body,
		HasImage:  m.ImageMime != "",
		CreatedAt: time.UnixMilli(m.CreatedMs).UTC().Format(time.RFC3339),
		CreatedMs: m.CreatedMs,
	}
	if out.HasImage {
		out.ImageURL = "/api/v1/files/" + m.ID
		out.ImageMime = m.ImageMime
	}
	return out
}

func channelFrom(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Channel")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("channel")); v != "" {
		return v
	}
	if r.MultipartForm != nil {
		if vs := r.MultipartForm.Value["channel"]; len(vs) > 0 && strings.TrimSpace(vs[0]) != "" {
			return strings.TrimSpace(vs[0])
		}
	}
	if v := strings.TrimSpace(r.PostForm.Get("channel")); v != "" {
		return v
	}
	return readCookie(r, "cn_channel")
}

func tokenFrom(r *http.Request) string {
	if v := r.Header.Get("X-Token"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("token"); v != "" {
		return v
	}
	if r.MultipartForm != nil {
		if vs := r.MultipartForm.Value["token"]; len(vs) > 0 && vs[0] != "" {
			return vs[0]
		}
	}
	if v := r.PostForm.Get("token"); v != "" {
		return v
	}
	return readCookie(r, "cn_token")
}

func formValue(r *http.Request, key string) string {
	if r.MultipartForm != nil {
		if vs := r.MultipartForm.Value[key]; len(vs) > 0 {
			return strings.TrimSpace(vs[0])
		}
	}
	return strings.TrimSpace(r.FormValue(key))
}

func readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func requestHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func sniffImage(b []byte) (string, bool) {
	if len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff {
		return "image/jpeg", true
	}
	if len(b) >= 8 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4e && b[3] == 0x47 && b[4] == 0x0d && b[5] == 0x0a && b[6] == 0x1a && b[7] == 0x0a {
		return "image/png", true
	}
	if len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a") {
		return "image/gif", true
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp", true
	}
	return "", false
}

func tooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func writeAuth(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "口令不正确")
	case errors.Is(err, ErrBadToken):
		writeError(w, http.StatusBadRequest, "口令请使用英文字母或数字，不要空格")
	case errors.Is(err, ErrBadChannel):
		writeError(w, http.StatusBadRequest, "教室编号请用 1–32 个字符，不要空格")
	default:
		writeError(w, http.StatusBadRequest, "无法进入")
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *rateLimiter) allow(key string, limit int, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	cutoff := now.Add(-time.Minute)
	fresh := make([]time.Time, 0, len(l.hits[key])+1)
	for _, ts := range l.hits[key] {
		if ts.After(cutoff) {
			fresh = append(fresh, ts)
		}
	}
	if len(fresh) >= limit {
		l.hits[key] = fresh
		return false
	}
	l.hits[key] = append(fresh, now)
	return true
}
