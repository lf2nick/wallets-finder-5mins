// wallets-finder-5mins backend — entry point
//
// 啟動順序：
//  1. 載入 ENV (DB_*, HTTP_ADDR, DASHBOARD_TOKEN)
//  2. 連 PostgreSQL（poly_tracker DB）
//  3. 跑 wf5m_* schema migration
//  4. 啟動 HTTP server
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lf2nick/wallets-finder-5mins/backend/internal/api"
	"github.com/lf2nick/wallets-finder-5mins/backend/internal/db"
)

func main() {
	cfg := loadConfig()

	database, err := db.Open(cfg.DBDSN)
	if err != nil {
		log.Fatalf("[wf5m] DB 連線失敗: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(context.Background()); err != nil {
		log.Fatalf("[wf5m] migration 失敗: %v", err)
	}

	srv := api.NewServer(database, cfg.DashboardToken)

	ctx, cancel := signalContext()
	defer cancel()

	go func() {
		if err := srv.ListenAndServe(ctx, cfg.HTTPAddr); err != nil {
			log.Fatalf("[wf5m] HTTP server 終止: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("[wf5m] 收到 shutdown signal, 等 3s 收尾...")
	time.Sleep(3 * time.Second)
}

type config struct {
	DBDSN          string
	HTTPAddr       string
	DashboardToken string
}

func loadConfig() config {
	c := config{
		HTTPAddr:       envOr("HTTP_ADDR", ":8080"),
		DashboardToken: os.Getenv("DASHBOARD_TOKEN"),
	}
	host := envOr("DB_HOST", "localhost")
	port := envOr("DB_PORT", "5432")
	user := envOr("DB_USER", "ming")
	pass := os.Getenv("DB_PASSWORD")
	name := envOr("DB_NAME", "poly_tracker")
	c.DBDSN = "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + name + "?sslmode=disable"
	if c.DashboardToken == "" {
		log.Printf("[wf5m] ⚠️  DASHBOARD_TOKEN 未設定 — API 無密碼保護")
	} else {
		log.Printf("[wf5m] 🔐 dashboard 密碼保護已啟用")
	}
	return c
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
