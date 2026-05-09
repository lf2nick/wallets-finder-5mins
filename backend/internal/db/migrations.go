package db

import (
	"context"
	"fmt"
	"log"
)

// Migrate 建立 wf5m_* 表 + 預設 config row。
// 重複跑安全（IF NOT EXISTS / ON CONFLICT DO NOTHING）。
func (db *DB) Migrate(ctx context.Context) error {
	stmts := []string{
		// filter 當前值（hot-reload）
		`CREATE TABLE IF NOT EXISTS wf5m_config (
			key        TEXT PRIMARY KEY,
			value      DOUBLE PRECISION NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// 設定變動 audit log
		`CREATE TABLE IF NOT EXISTS wf5m_config_history (
			id         BIGSERIAL PRIMARY KEY,
			key        TEXT NOT NULL,
			old_value  DOUBLE PRECISION,
			new_value  DOUBLE PRECISION NOT NULL,
			changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wf5m_config_history_at ON wf5m_config_history(changed_at DESC)`,
		// 每次 scan snapshot
		`CREATE TABLE IF NOT EXISTS wf5m_snapshots (
			id              BIGSERIAL PRIMARY KEY,
			snapshot_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			config_json     JSONB NOT NULL,
			match_count     INT NOT NULL,
			candidate_count INT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wf5m_snapshots_at ON wf5m_snapshots(snapshot_at DESC)`,
		// 每筆 snapshot 命中明細（denormalized stats — 凍結當下值）
		`CREATE TABLE IF NOT EXISTS wf5m_snapshot_matches (
			snapshot_id          BIGINT NOT NULL REFERENCES wf5m_snapshots(id) ON DELETE CASCADE,
			address              TEXT NOT NULL,
			win_rate             DOUBLE PRECISION,
			crypto_ratio         DOUBLE PRECISION,
			dual_market_ratio    DOUBLE PRECISION,
			hold_to_settle_ratio DOUBLE PRECISION,
			total_trades         INT,
			net_pnl_usd          DOUBLE PRECISION,
			PRIMARY KEY (snapshot_id, address)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wf5m_snapshot_matches_addr ON wf5m_snapshot_matches(address)`,
	}
	for i, s := range stmts {
		if _, err := db.sql.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migration step %d: %w", i, err)
		}
	}
	// seed default config（只在 row 不存在時插入）
	for _, c := range defaultConfig() {
		if _, err := db.sql.ExecContext(ctx,
			`INSERT INTO wf5m_config (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`,
			c.key, c.value); err != nil {
			return fmt.Errorf("seed config %s: %w", c.key, err)
		}
	}
	log.Printf("[wf5m] migration ok — wf5m_* tables ready")
	return nil
}

type defaultCfg struct {
	key   string
	value float64
}

func defaultConfig() []defaultCfg {
	return []defaultCfg{
		{"dual_market_ratio_max", 25},   // 雙向持倉 ≤ 25%
		{"crypto_ratio_min", 80},        // 5m 比例 ≥ 80%
		{"hold_to_settle_ratio_min", 80}, // hold 到結算 ≥ 80%
		{"min_trades", 30},              // 最少筆數
	}
}
