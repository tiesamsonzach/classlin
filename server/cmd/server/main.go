package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"classroom-notify/internal/api"
	"classroom-notify/internal/hub"
	"classroom-notify/internal/store"
)

func main() {
	port := env("PORT", "8080")
	if _, err := strconv.Atoi(port); err != nil {
		log.Fatal("PORT 必须是数字")
	}
	dataDir := env("DATA_DIR", "data")
	token := strings.TrimSpace(os.Getenv("TOKEN"))
	if err := api.CheckConfiguredToken(token); err != nil {
		log.Fatal(err)
	}
	bind := env("BIND", "0.0.0.0")

	st, err := store.Open(dataDir)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	admins, err := api.ParseAdmins(os.Getenv("ADMIN_USERS"), os.Getenv("ADMIN_USER"), os.Getenv("ADMIN_PASSWORD"), os.Getenv("ADMIN_CLASSROOMS"))
	if err != nil {
		log.Fatal(err)
	}
	ice, err := api.ParseICEConfig(
		os.Getenv("STUN_URLS"),
		os.Getenv("TURN_URLS"),
		os.Getenv("TURN_USERNAME"),
		os.Getenv("TURN_PASSWORD"),
		os.Getenv("TURN_SECRET"),
		os.Getenv("TURN_TTL_SECONDS"),
	)
	if err != nil {
		log.Fatal(err)
	}
	secret, err := sessionSecret(dataDir)
	if err != nil {
		log.Fatalf("会话密钥: %v", err)
	}
	apiServer := api.New(st, hub.New(), token)
	apiServer.ConfigureCalls(admins, ice, secret)

	srv := &http.Server{
		Addr:              bind + ":" + port,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()

	log.Printf("课堂通知已启动 监听=%s 数据目录=%s 需要口令=%t 管理员=%t TURN=%t", srv.Addr, dataDir, token != "", admins.Configured(), len(ice.TURNURLs) > 0)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func sessionSecret(dataDir string) ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("ADMIN_SESSION_SECRET")); v != "" {
		if len(v) < 16 {
			return nil, errors.New("ADMIN_SESSION_SECRET 至少 16 位")
		}
		return []byte(v), nil
	}
	path := filepath.Join(dataDir, "admin-session.key")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return []byte(strings.TrimSpace(string(b))), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	encoded := []byte(base64.RawStdEncoding.EncodeToString(buf))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, err
	}
	return encoded, nil
}
