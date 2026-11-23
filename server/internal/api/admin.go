package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	adminCookieName = "cn_admin"
	sessionTTL      = 12 * time.Hour
)

var (
	ErrNoAdmin       = errors.New("no admin")
	ErrNotAdmin      = errors.New("not admin")
	dummyPasswordSum = sha256.Sum256([]byte("classroom-notify-unused-admin-password"))
)

type adminAccount struct {
	name     string
	passHash [32]byte
	allRooms bool
	rooms    map[string]struct{}
}

// AdminDir is the set of accounts that may place a video call.
// Classroom PCs are not in this directory; they stay identified by room code.
type AdminDir struct {
	users    map[string]adminAccount
	restrict bool
}

func (a AdminDir) Configured() bool {
	return len(a.users) > 0
}

func (a AdminDir) Has(username string) bool {
	_, ok := a.users[username]
	return ok
}

// Allow reports whether a logged-in admin may call this classroom.
// When ADMIN_CLASSROOMS is unset, any logged-in admin may call any room.
// When it is set, only listed rooms (or "*") are allowed.
func (a AdminDir) Allow(username, channel string) bool {
	acc, ok := a.users[username]
	if !ok {
		return false
	}
	if !a.restrict || acc.allRooms {
		return true
	}
	_, ok = acc.rooms[channel]
	return ok
}

func (a AdminDir) View(username string) (all bool, rooms []string) {
	acc, ok := a.users[username]
	if !ok {
		return false, nil
	}
	if !a.restrict || acc.allRooms {
		return true, nil
	}
	rooms = make([]string, 0, len(acc.rooms))
	for room := range acc.rooms {
		rooms = append(rooms, room)
	}
	return false, rooms
}

func (a AdminDir) Authenticate(username, password string) (string, bool) {
	username = strings.TrimSpace(username)
	sum := sha256.Sum256([]byte(password))
	acc, ok := a.users[username]
	hash := dummyPasswordSum
	if ok {
		hash = acc.passHash
	}
	if subtle.ConstantTimeCompare(sum[:], hash[:]) != 1 || !ok {
		return "", false
	}
	return acc.name, true
}

// ParseAdmins reads ADMIN_USERS / ADMIN_USER+ADMIN_PASSWORD and ADMIN_CLASSROOMS.
//
// ADMIN_USERS is "user:password,user2:password2". Passwords may contain colons
// but not commas. ADMIN_USER and ADMIN_PASSWORD add one more account.
// ADMIN_CLASSROOMS is "user:ROOM1|ROOM2,user2:*". Empty means every logged-in
// admin may call every classroom.
func ParseAdmins(users, singleUser, singlePass, rooms string) (AdminDir, error) {
	dir := AdminDir{users: map[string]adminAccount{}}
	add := func(user, pass string) error {
		user = strings.TrimSpace(user)
		if !validAdminUser(user) {
			return errors.New("管理员用户名请用 1–32 位字母、数字、点、下划线或减号")
		}
		if pass == "" || len(pass) > 128 {
			return errors.New("管理员密码不能为空，最长 128 位")
		}
		if strings.ContainsAny(pass, "\r\n") {
			return errors.New("管理员密码不能换行")
		}
		dir.users[user] = adminAccount{name: user, passHash: sha256.Sum256([]byte(pass))}
		return nil
	}
	users = strings.TrimSpace(users)
	if users != "" {
		for _, part := range strings.Split(users, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			user, pass, ok := strings.Cut(part, ":")
			if !ok {
				return AdminDir{}, errors.New("ADMIN_USERS 每一项都要是 用户名:密码")
			}
			if err := add(user, pass); err != nil {
				return AdminDir{}, err
			}
		}
	}
	singleUser = strings.TrimSpace(singleUser)
	if singleUser != "" || strings.TrimSpace(singlePass) != "" {
		if singleUser == "" || singlePass == "" {
			return AdminDir{}, errors.New("ADMIN_USER 和 ADMIN_PASSWORD 必须同时设置")
		}
		if err := add(singleUser, singlePass); err != nil {
			return AdminDir{}, err
		}
	}
	rooms = strings.TrimSpace(rooms)
	if rooms == "" {
		return dir, nil
	}
	dir.restrict = true
	seen := map[string]struct{}{}
	for _, part := range strings.Split(rooms, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		user, list, ok := strings.Cut(part, ":")
		if !ok {
			return AdminDir{}, errors.New("ADMIN_CLASSROOMS 每一项都要是 用户名:教室编号")
		}
		user = strings.TrimSpace(user)
		acc, ok := dir.users[user]
		if !ok {
			return AdminDir{}, errors.New("ADMIN_CLASSROOMS 里的用户不在管理员账号中: " + user)
		}
		seen[user] = struct{}{}
		list = strings.TrimSpace(list)
		if list == "*" {
			acc.allRooms = true
			acc.rooms = nil
			dir.users[user] = acc
			continue
		}
		acc.rooms = map[string]struct{}{}
		for _, room := range strings.Split(list, "|") {
			room = strings.TrimSpace(room)
			if room == "" {
				continue
			}
			if !ValidChannel(room) {
				return AdminDir{}, errors.New("ADMIN_CLASSROOMS 里的教室编号不合法: " + room)
			}
			acc.rooms[room] = struct{}{}
		}
		if len(acc.rooms) == 0 {
			return AdminDir{}, errors.New("ADMIN_CLASSROOMS 里 " + user + " 没有教室")
		}
		dir.users[user] = acc
	}
	if len(seen) == 0 {
		return AdminDir{}, errors.New("ADMIN_CLASSROOMS 是空的")
	}
	return dir, nil
}

func validAdminUser(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Admins.Configured() {
		writeError(w, http.StatusForbidden, "服务器还没有配置管理员，不能登录")
		return
	}
	ip := clientIP(r)
	if !s.limiter.allow("admin-login:"+ip, 30, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "尝试太多次，请稍后再登录")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请填写账号和密码")
		return
	}
	user, ok := s.Admins.Authenticate(body.Username, body.Password)
	if !ok {
		writeError(w, http.StatusUnauthorized, "账号或密码不正确")
		return
	}
	s.setAdminCookie(w, r, user)
	all, rooms := s.Admins.View(user)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"username":      user,
		"allClassrooms": all,
		"classrooms":    rooms,
	})
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestHTTPS(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminSession(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"configured":    s.Admins.Configured(),
		"authenticated": false,
	}
	user, err := s.adminFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	all, rooms := s.Admins.View(user)
	out["authenticated"] = true
	out["username"] = user
	out["allClassrooms"] = all
	out["classrooms"] = rooms
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setAdminCookie(w http.ResponseWriter, r *http.Request, user string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    s.mintSession(user, time.Now()),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestHTTPS(r),
	})
}

func (s *Server) adminFromRequest(r *http.Request) (string, error) {
	if !s.Admins.Configured() {
		return "", ErrNoAdmin
	}
	c, err := r.Cookie(adminCookieName)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return "", ErrNotAdmin
	}
	user, err := s.parseSession(c.Value, time.Now())
	if err != nil || !s.Admins.Has(user) {
		return "", ErrNotAdmin
	}
	return user, nil
}

func (s *Server) mintSession(user string, now time.Time) string {
	payload := user + "|" + strconv.FormatInt(now.Add(sessionTTL).Unix(), 10)
	mac := hmac.New(sha256.New, s.sessionKey())
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func (s *Server) parseSession(token string, now time.Time) (string, error) {
	enc, sig, ok := strings.Cut(token, ".")
	if !ok || enc == "" || sig == "" {
		return "", ErrNotAdmin
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", ErrNotAdmin
	}
	mac := hmac.New(sha256.New, s.sessionKey())
	_, _ = mac.Write(payload)
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return "", ErrNotAdmin
	}
	user, expRaw, ok := strings.Cut(string(payload), "|")
	if !ok || !validAdminUser(user) {
		return "", ErrNotAdmin
	}
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || now.Unix() > exp {
		return "", ErrNotAdmin
	}
	return user, nil
}

func (s *Server) sessionKey() []byte {
	if len(s.sessionSecret) > 0 {
		return s.sessionSecret
	}
	return []byte("unset-session-secret")
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}

func newSessionSecret() []byte {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return []byte("fallback-session-secret-change-me")
	}
	return buf
}
