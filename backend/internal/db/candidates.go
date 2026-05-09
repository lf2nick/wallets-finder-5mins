package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Candidate 是 crypto_wallet_candidates 一筆的子集（只取我們 finder 用得到的欄位）。
// 不 SELECT *，避免 poly-tracker 改 schema 把這邊弄壞（依 user 的 review）。
type Candidate struct {
	Address           string    `json:"address"`
	TotalTrades       int       `json:"total_trades"`
	WinCount          int       `json:"win_count"`
	LoseCount         int       `json:"lose_count"`
	WinRate           float64   `json:"win_rate"`
	ROIPct            float64   `json:"roi_pct"`
	NetPnLUSD         float64   `json:"net_pnl_usd"`
	VolumeUSD         float64   `json:"volume_usd"`
	CryptoRatio       float64   `json:"crypto_ratio"`
	DualMarketRatio   float64   `json:"dual_market_ratio"`
	HoldToSettleRatio float64   `json:"hold_to_settle_ratio"`
	AvgBuyOffsetSec   float64   `json:"avg_buy_offset_sec"`
	ProfitLossRatio   float64   `json:"profit_loss_ratio"`
	WashRatio         float64   `json:"wash_ratio"`
	LastTradeAt       time.Time `json:"last_trade_at,omitempty"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
	AlreadyObserved   bool      `json:"already_observed"` // 已在 copy_sim_wallets？
}

// CandidateFilter 是「找 wallet」的條件組合。
// 全 nil = 不過濾（debug 用）；正常 user 流程會塞當前 wf5m_config 值。
type CandidateFilter struct {
	DualMaxPct      float64 // dual_market_ratio ≤ 此值
	CryptoMinPct    float64 // crypto_ratio ≥ 此值
	HoldMinPct      float64 // hold_to_settle_ratio ≥ 此值
	MinTrades       int     // total_trades ≥ 此值
	SortBy          string  // "net_pnl_usd" / "win_rate" / "roi_pct" — 預設 net_pnl_usd
	Limit           int     // 預設 500
}

// FindCandidates 套用 filter 撈 candidates。
// LEFT JOIN copy_sim_wallets 標 already_observed。
func (db *DB) FindCandidates(ctx context.Context, f CandidateFilter) ([]Candidate, error) {
	sortBy := "net_pnl_usd"
	switch f.SortBy {
	case "win_rate", "roi_pct", "total_trades", "hold_to_settle_ratio":
		sortBy = f.SortBy
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	q := fmt.Sprintf(`
		SELECT c.address, c.total_trades, c.win_count, c.lose_count,
		       c.win_rate, c.roi_pct, c.net_pnl_usd, c.volume_usd,
		       c.crypto_ratio, c.dual_market_ratio, c.hold_to_settle_ratio,
		       c.avg_buy_offset_sec, c.profit_loss_ratio, c.wash_ratio,
		       c.last_trade_at, c.evaluated_at,
		       (s.address IS NOT NULL) AS already_observed
		FROM crypto_wallet_candidates c
		LEFT JOIN copy_sim_wallets s ON LOWER(s.address) = c.address
		WHERE c.dual_market_ratio   <= $1
		  AND c.crypto_ratio        >= $2
		  AND c.hold_to_settle_ratio >= $3
		  AND c.total_trades        >= $4
		ORDER BY c.%s DESC
		LIMIT $5
	`, sortBy)

	rows, err := db.sql.QueryContext(ctx, q,
		f.DualMaxPct, f.CryptoMinPct, f.HoldMinPct, f.MinTrades, limit)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		var lt sql.NullTime
		if err := rows.Scan(&c.Address, &c.TotalTrades, &c.WinCount, &c.LoseCount,
			&c.WinRate, &c.ROIPct, &c.NetPnLUSD, &c.VolumeUSD,
			&c.CryptoRatio, &c.DualMarketRatio, &c.HoldToSettleRatio,
			&c.AvgBuyOffsetSec, &c.ProfitLossRatio, &c.WashRatio,
			&lt, &c.EvaluatedAt, &c.AlreadyObserved); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if lt.Valid {
			c.LastTradeAt = lt.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountCandidates 給 snapshot 用 — 知道 candidates 池總大小（沒 filter 的全部 row 數）。
func (db *DB) CountCandidates(ctx context.Context) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM crypto_wallet_candidates`).Scan(&n)
	return n, err
}
