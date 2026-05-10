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
	"strconv"
	"syscall"
	"time"

	"github.com/lf2nick/wallets-finder-5mins/backend/internal/api"
	"github.com/lf2nick/wallets-finder-5mins/backend/internal/db"
	"github.com/lf2nick/wallets-finder-5mins/backend/internal/scanner"
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

	scn := scanner.New(database)
	srv := api.NewServer(database, scn, cfg.DashboardToken)

	ctx, cancel := signalContext()
	defer cancel()

	go func() {
		if err := srv.ListenAndServe(ctx, cfg.HTTPAddr); err != nil {
			log.Fatalf("[wf5m] HTTP server 終止: %v", err)
		}
	}()

	// 自動 scan ticker（預設 0 = off，避免跟 poly-tracker 過渡期同時跑）
	if cfg.AutoScanHours > 0 {
		go runAutoScanTicker(ctx, scn, cfg.AutoScanHours)
	} else {
		log.Printf("[wf5m] auto-scan 未啟用（WF5M_AUTO_SCAN_HOURS=0）— 只能手動觸發 /api/cryptofinder/scan")
	}

	<-ctx.Done()
	log.Printf("[wf5m] 收到 shutdown signal, 等 3s 收尾...")
	time.Sleep(3 * time.Second)
}

// runAutoScanTicker 每 N 小時自動 trigger 一次 scan。
// 過渡期建議設 0（off）；確認 poly-tracker 那邊的 auto 拔掉後再開。
func runAutoScanTicker(ctx context.Context, scn *scanner.Scanner, hours int) {
	interval := time.Duration(hours) * time.Hour
	log.Printf("[wf5m] auto-scan 已啟用：每 %v 跑一次 cryptofinder", interval)
	// 啟動延遲 5 分鐘（避免 deploy 完馬上跑、跟手動 scan 撞）
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Minute):
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	doScan := func() {
		if scn.IsRunning() {
			log.Printf("[wf5m] auto-scan tick 跳過：上一輪還在跑")
			return
		}
		log.Printf("[wf5m] auto-scan tick 啟動")
		scanCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		seed, cand, err := scn.Scan(scanCtx, 1, 14, 30) // seed 近 1 天，績效評估近 14 天
		cancel()
		if err != nil {
			log.Printf("[wf5m] auto-scan 失敗: %v", err)
			return
		}
		log.Printf("[wf5m] auto-scan 完成 seed=%d cand=%d", seed, cand)
	}
	doScan() // 啟動延遲後第一次跑
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			doScan()
		}
	}
}

type config struct {
	DBDSN          string
	HTTPAddr       string
	DashboardToken string
	AutoScanHours  int // 0 = off
}

func loadConfig() config {
	c := config{
		HTTPAddr:       envOr("HTTP_ADDR", ":8080"),
		DashboardToken: os.Getenv("DASHBOARD_TOKEN"),
		AutoScanHours:  parseEnvInt("WF5M_AUTO_SCAN_HOURS", 0),
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

func parseEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
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
