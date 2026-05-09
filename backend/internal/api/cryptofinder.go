package api

import (
	"context"
	"log"
	"net/http"
)

// POST /api/cryptofinder/scan?days=14&min_trades=30
//
// 立刻 trigger 一次 scan（背景跑，不阻塞 HTTP request）。
// 如果已有 scan 在跑，回 409 Conflict。
//
// 預估時間：3-8 分鐘（看 4 個 asset 活躍度 + seed 數量）。
// 前端應該 poll /api/cryptofinder/status 看 phase 進度。
func (s *Server) handleCryptofinderScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.scanner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scanner not initialized"})
		return
	}
	if s.scanner.IsRunning() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "scan already running"})
		return
	}
	q := r.URL.Query()
	days := 14
	minTrades := 30
	if v := parseInt(q.Get("days"), 0); v > 0 && v <= 90 {
		days = v
	}
	if v := parseInt(q.Get("min_trades"), 0); v > 0 && v <= 10000 {
		minTrades = v
	}
	// 跑在背景：HTTP request 不能 block 5 分鐘等 scan 完
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*60*1000*1000*1000) // 30 min
		defer cancel()
		seedCount, candCount, err := s.scanner.Scan(ctx, days, minTrades)
		if err != nil {
			log.Printf("[wf5m/scan] 失敗: %v", err)
			return
		}
		log.Printf("[wf5m/scan] ✅ 完成 seed=%d cand=%d", seedCount, candCount)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"days":       days,
		"min_trades": minTrades,
		"message":    "scan triggered — poll /api/cryptofinder/status",
	})
}

// GET /api/cryptofinder/status — 當前 scan progress + 上次完成的 run 紀錄
func (s *Server) handleCryptofinderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]any{}
	if s.scanner != nil {
		resp["running"] = s.scanner.IsRunning()
		resp["progress"] = s.scanner.SnapshotProgress()
	}
	if last, err := s.db.LastCryptoFinderRun(r.Context()); err == nil && last != nil {
		resp["last_run"] = last
	}
	writeJSON(w, http.StatusOK, resp)
}
