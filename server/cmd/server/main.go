package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	srv := &http.Server{
		Addr:              bind + ":" + port,
		Handler:           api.New(st, hub.New(), token).Handler(),
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

	log.Printf("课堂通知已启动 监听=%s 数据目录=%s 需要口令=%t", srv.Addr, dataDir, token != "")
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
