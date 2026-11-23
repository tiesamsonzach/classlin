package api

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ICEConfig is the STUN/TURN list handed to browsers.
// TURN is optional. Without TURN_URLS the intercom stays on public STUN.
type ICEConfig struct {
	STUNURLs []string
	TURNURLs []string
	Username string
	Password string
	Secret   string
	TTL      time.Duration
}

func DefaultICE() ICEConfig {
	return ICEConfig{
		STUNURLs: []string{"stun:stun.l.google.com:19302"},
		TTL:      time.Hour,
	}
}

// ParseICEConfig reads STUN_URLS, TURN_URLS, TURN_USERNAME, TURN_PASSWORD,
// TURN_SECRET and TURN_TTL_SECONDS. TURN_SECRET mints short-lived coturn
// REST credentials (use-auth-secret). Static username/password are used when
// the secret is empty. TURN_URLS without either kind of credential is rejected
// so the server never advertises an open relay.
func ParseICEConfig(stun, turn, user, pass, secret, ttlRaw string) (ICEConfig, error) {
	cfg := DefaultICE()
	if strings.TrimSpace(stun) != "" {
		urls, err := splitICEURLs(stun, "stun")
		if err != nil {
			return ICEConfig{}, err
		}
		cfg.STUNURLs = urls
	}
	cfg.Username = strings.TrimSpace(user)
	cfg.Password = pass
	cfg.Secret = strings.TrimSpace(secret)
	cfg.TTL = time.Hour
	if strings.TrimSpace(ttlRaw) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(ttlRaw))
		if err != nil || n < 60 || n > 86400 {
			return ICEConfig{}, errICE("TURN_TTL_SECONDS 必须是 60 到 86400 的秒数")
		}
		cfg.TTL = time.Duration(n) * time.Second
	}
	if strings.TrimSpace(turn) == "" {
		return cfg, nil
	}
	urls, err := splitICEURLs(turn, "turn")
	if err != nil {
		return ICEConfig{}, err
	}
	if cfg.Secret == "" && (cfg.Username == "" || cfg.Password == "") {
		return ICEConfig{}, errICE("设置了 TURN_URLS 时，必须提供 TURN_SECRET，或同时提供 TURN_USERNAME 和 TURN_PASSWORD")
	}
	cfg.TURNURLs = urls
	return cfg, nil
}

type iceError string

func (e iceError) Error() string { return string(e) }

func errICE(msg string) error { return iceError(msg) }

func splitICEURLs(raw, kind string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch kind {
		case "stun":
			if !strings.HasPrefix(part, "stun:") && !strings.HasPrefix(part, "stuns:") {
				return nil, errICE("STUN_URLS 只能是 stun: 或 stuns: 地址")
			}
		case "turn":
			if !strings.HasPrefix(part, "turn:") && !strings.HasPrefix(part, "turns:") {
				return nil, errICE("TURN_URLS 只能是 turn: 或 turns: 地址")
			}
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil, errICE(kind + " 地址是空的")
	}
	return out, nil
}

func (cfg ICEConfig) Servers(identity string, now time.Time) (servers []map[string]any, ttl int) {
	if len(cfg.STUNURLs) == 0 {
		cfg.STUNURLs = DefaultICE().STUNURLs
	}
	servers = append(servers, map[string]any{"urls": cfg.STUNURLs})
	ttl = int(cfg.TTL.Seconds())
	if ttl <= 0 {
		ttl = 3600
	}
	if len(cfg.TURNURLs) == 0 {
		return servers, ttl
	}
	entry := map[string]any{"urls": cfg.TURNURLs}
	if cfg.Secret != "" {
		user, cred := mintTURNRest(cfg.Secret, identity, now.Add(cfg.TTL))
		entry["username"] = user
		entry["credential"] = cred
	} else {
		entry["username"] = cfg.Username
		entry["credential"] = cfg.Password
	}
	servers = append(servers, entry)
	return servers, ttl
}

// mintTURNRest builds coturn use-auth-secret credentials:
// username = "<expiry-unix>:<name>", password = base64(HMAC-SHA1(secret, username)).
func mintTURNRest(secret, name string, expiry time.Time) (string, string) {
	name = sanitizeTURNName(name)
	username := strconv.FormatInt(expiry.Unix(), 10) + ":" + name
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(username))
	return username, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func sanitizeTURNName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "classroom"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "classroom"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func (s *Server) getICE(w http.ResponseWriter, r *http.Request) {
	identity := "classroom"
	if user, err := s.adminFromRequest(r); err == nil {
		identity = user
	} else if _, err := s.authorize(channelFrom(r), tokenFrom(r)); err != nil {
		writeAuth(w, err)
		return
	}
	servers, ttl := s.ICE.Servers(identity, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{
		"iceServers": servers,
		"ttlSeconds": ttl,
	})
}
