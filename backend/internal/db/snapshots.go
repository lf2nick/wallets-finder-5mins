package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Snapshot 是 wf5m_snapshots 一筆。
type Snapshot struct {
	ID             int64                  `json:"id"`
	SnapshotAt     time.Time              `json:"snapshot_at"`
	Config         map[string]float64     `json:"config"`
	MatchCount     int                    `json:"match_count"`
	CandidateCount int                    `json:"candidate_count"`
}

// SnapshotMatch 是一筆 snapshot 中的 wallet 命中明細（denormalized）。
type SnapshotMatch struct {
	SnapshotID        int64     `json:"snapshot_id"`
	Address           string    `json:"address"`
	WinRate           *float64  `json:"win_rate,omitempty"`
	CryptoRatio       *float64  `json:"crypto_ratio,omitempty"`
	DualMarketRatio   *float64  `json:"dual_market_ratio,omitempty"`
	HoldToSettleRatio *float64  `json:"hold_to_settle_ratio,omitempty"`
	TotalTrades       *int      `json:"total_trades,omitempty"`
	NetPnLUSD         *float64  `json:"net_pnl_usd,omitempty"`
	SnapshotAt        time.Time `json:"snapshot_at,omitempty"` // 給 wallet history 用
}

// CreateSnapshot 寫一筆 snapshot 包 matches。Atomic via tx。
// candidates 應該已經 filter 過（caller 套用當前 config 撈完）。
func (db *DB) CreateSnapshot(ctx context.Context, cfg map[string]float64, candidateCount int, matches []Candidate) (int64, error) {
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return 0, fmt.Errorf("marshal config: %w", err)
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO wf5m_snapshots (config_json, match_count, candidate_count)
		VALUES ($1, $2, $3)
		RETURNING id
	`, cfgJSON, len(matches), candidateCount).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert snapshot: %w", err)
	}

	if len(matches) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO wf5m_snapshot_matches
			  (snapshot_id, address, win_rate, crypto_ratio, dual_market_ratio,
			   hold_to_settle_ratio, total_trades, net_pnl_usd)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`)
		if err != nil {
			return 0, fmt.Errorf("prepare match insert: %w", err)
		}
		defer stmt.Close()
		for _, c := range matches {
			if _, err := stmt.ExecContext(ctx, id, c.Address,
				c.WinRate, c.CryptoRatio, c.DualMarketRatio,
				c.HoldToSettleRatio, c.TotalTrades, c.NetPnLUSD); err != nil {
				return 0, fmt.Errorf("insert match %s: %w", c.Address, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// ListSnapshots 撈最近 N 筆 snapshot 摘要（不含 matches 明細）。
func (db *DB) ListSnapshots(ctx context.Context, limit int) ([]Snapshot, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, snapshot_at, config_json, match_count, candidate_count
		FROM wf5m_snapshots
		ORDER BY snapshot_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var s Snapshot
		var cfgRaw []byte
		if err := rows.Scan(&s.ID, &s.SnapshotAt, &cfgRaw, &s.MatchCount, &s.CandidateCount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cfgRaw, &s.Config)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSnapshotMatches 拿一筆 snapshot 的所有命中 wallet。
func (db *DB) GetSnapshotMatches(ctx context.Context, snapshotID int64) ([]SnapshotMatch, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT snapshot_id, address, win_rate, crypto_ratio, dual_market_ratio,
		       hold_to_settle_ratio, total_trades, net_pnl_usd
		FROM wf5m_snapshot_matches
		WHERE snapshot_id = $1
		ORDER BY net_pnl_usd DESC NULLS LAST
	`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotMatch
	for rows.Next() {
		var m SnapshotMatch
		if err := rows.Scan(&m.SnapshotID, &m.Address,
			&m.WinRate, &m.CryptoRatio, &m.DualMarketRatio,
			&m.HoldToSettleRatio, &m.TotalTrades, &m.NetPnLUSD); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetWalletHistory 拿單一 wallet 過去 N 次命中歷史（join snapshot_at）。
func (db *DB) GetWalletHistory(ctx context.Context, address string, limit int) ([]SnapshotMatch, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT m.snapshot_id, m.address, m.win_rate, m.crypto_ratio, m.dual_market_ratio,
		       m.hold_to_settle_ratio, m.total_trades, m.net_pnl_usd, s.snapshot_at
		FROM wf5m_snapshot_matches m
		JOIN wf5m_snapshots s ON s.id = m.snapshot_id
		WHERE LOWER(m.address) = LOWER($1)
		ORDER BY s.snapshot_at DESC
		LIMIT $2
	`, address, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotMatch
	for rows.Next() {
		var m SnapshotMatch
		if err := rows.Scan(&m.SnapshotID, &m.Address,
			&m.WinRate, &m.CryptoRatio, &m.DualMarketRatio,
			&m.HoldToSettleRatio, &m.TotalTrades, &m.NetPnLUSD, &m.SnapshotAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
