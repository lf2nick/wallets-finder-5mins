package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lf2nick/wallets-finder-5mins/backend/internal/db"
)

// GET /api/candidates — 套用 query string 中的 filter（沒帶 = 用當前 wf5m_config）
//
//	?dual_max=25&crypto_min=80&hold_min=80&min_trades=30&sort=net_pnl_usd&limit=500
func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := s.db.GetAllConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	q := r.URL.Query()
	filter := db.CandidateFilter{
		DualMaxPct:   parseFloat(q.Get("dual_max"), cfg["dual_market_ratio_max"]),
		CryptoMinPct: parseFloat(q.Get("crypto_min"), cfg["crypto_ratio_min"]),
		HoldMinPct:   parseFloat(q.Get("hold_min"), cfg["hold_to_settle_ratio_min"]),
		MinTrades:    int(parseFloat(q.Get("min_trades"), cfg["min_trades"])),
		SortBy:       q.Get("sort"),
		Limit:        parseInt(q.Get("limit"), 500),
	}
	cands, err := s.db.FindCandidates(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	totalPool, _ := s.db.CountCandidates(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"items":           cands,
		"match_count":     len(cands),
		"candidate_count": totalPool,
		"filter":          filter,
	})
}

// GET /api/config — 當前 filter 設定
// PUT /api/config  body: {"key": "...", "value": 25}  — 更新單一 key
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.db.GetAllConfig(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut, http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4*1024))
		var req struct {
			Updates map[string]float64 `json:"updates"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse json: " + err.Error()})
			return
		}
		if len(req.Updates) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty updates"})
			return
		}
		// 簡單 sanity：值必須在合理 [0, 1000] 範圍
		for k, v := range req.Updates {
			if v < 0 || v > 1000 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid value for " + k})
				return
			}
		}
		for k, v := range req.Updates {
			if err := s.db.SetConfig(r.Context(), k, v); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		cfg, _ := s.db.GetAllConfig(r.Context())
		writeJSON(w, http.StatusOK, cfg)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /api/scan — 用當前 config 跑一輪篩選 + 寫 snapshot
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := s.db.GetAllConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	filter := db.CandidateFilter{
		DualMaxPct:   cfg["dual_market_ratio_max"],
		CryptoMinPct: cfg["crypto_ratio_min"],
		HoldMinPct:   cfg["hold_to_settle_ratio_min"],
		MinTrades:    int(cfg["min_trades"]),
		SortBy:       "net_pnl_usd",
		Limit:        2000, // snapshot 多抓一點，UI 顯示用 default 500
	}
	cands, err := s.db.FindCandidates(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	totalPool, _ := s.db.CountCandidates(r.Context())
	id, err := s.db.CreateSnapshot(r.Context(), cfg, totalPool, cands)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot_id":     id,
		"match_count":     len(cands),
		"candidate_count": totalPool,
	})
}

// GET /api/history/config?limit=100 — audit log
func (s *Server) handleHistoryConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 100)
	rows, err := s.db.GetConfigHistory(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

// GET /api/history/snapshots?limit=50 — snapshot 摘要清單
func (s *Server) handleHistorySnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 50)
	rows, err := s.db.ListSnapshots(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

// GET /api/history/snapshot?id=N — snapshot 命中明細
func (s *Server) handleHistorySnapshotDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	rows, err := s.db.GetSnapshotMatches(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

// GET /api/history/wallet?address=0x...&limit=50 — wallet 命中歷史
func (s *Server) handleHistoryWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid address"})
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 50)
	rows, err := s.db.GetWalletHistory(r.Context(), addr, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

// POST /api/observe  body: {"address": "0x...", "alias": "..."}
// → 寫入 poly-tracker 的 copy_sim_wallets 表，立即觀察
func (s *Server) handleObserve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4*1024))
	var req struct {
		Address string `json:"address"`
		Alias   string `json:"alias"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse json: " + err.Error()})
		return
	}
	if err := s.db.AddCopySimWallet(r.Context(), req.Address, req.Alias); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "address": strings.ToLower(req.Address)})
}

// helpers

func parseFloat(s string, def float64) float64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
