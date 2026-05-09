// Package api 是 wf5m HTTP server 的 handlers + routing。
//
// 全部 /api/* 都包 auth middleware（Bearer token from DASHBOARD_TOKEN）。
// 空 token = 不啟用密碼保護（dev 用）。
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lf2nick/wallets-finder-5mins/backend/internal/db"
)

type Server struct {
	db             *db.DB
	dashboardToken string
}

func NewServer(database *db.DB, token string) *Server {
	return &Server{db: database, dashboardToken: token}
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", s.handleHealth) // 不過 auth — 給 docker healthcheck

	mux.HandleFunc("/api/candidates", s.auth(s.handleCandidates))
	mux.HandleFunc("/api/config", s.auth(s.handleConfig))
	mux.HandleFunc("/api/scan", s.auth(s.handleScan))

	mux.HandleFunc("/api/history/config", s.auth(s.handleHistoryConfig))
	mux.HandleFunc("/api/history/snapshots", s.auth(s.handleHistorySnapshots))
	mux.HandleFunc("/api/history/snapshot", s.auth(s.handleHistorySnapshotDetail)) // ?id=N
	mux.HandleFunc("/api/history/wallet", s.auth(s.handleHistoryWallet))            // ?address=0x...

	mux.HandleFunc("/api/observe", s.auth(s.handleObserve)) // POST {address, alias}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("[wf5m] HTTP server listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// auth 對應 poly-tracker 的 auth middleware — 同 Bearer token 邏輯
// 也接 ?token= query 給 SSE / 簡易 debug 用（v1 沒 SSE 但留著）。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.dashboardToken == "" {
			next(w, r)
			return
		}
		got := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			got = strings.TrimPrefix(h, "Bearer ")
		}
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if got != s.dashboardToken {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().Unix()})
}
