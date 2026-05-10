package db

import (
	"context"
	"fmt"
	"log"
)

// Migrate 建立 wf5m 自己的表，並補齊共用的 crypto finder 表。
func (db *DB) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS wf5m_config (
			key        TEXT PRIMARY KEY,
			value      DOUBLE PRECISION NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS wf5m_config_history (
			id         BIGSERIAL PRIMARY KEY,
			key        TEXT NOT NULL,
			old_value  DOUBLE PRECISION,
			new_value  DOUBLE PRECISION NOT NULL,
			changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wf5m_config_history_at ON wf5m_config_history(changed_at DESC)`,
		`CREATE TABLE IF NOT EXISTS wf5m_snapshots (
			id              BIGSERIAL PRIMARY KEY,
			snapshot_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			config_json     JSONB NOT NULL,
			match_count     INT NOT NULL,
			candidate_count INT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wf5m_snapshots_at ON wf5m_snapshots(snapshot_at DESC)`,
		`CREATE TABLE IF NOT EXISTS wf5m_snapshot_matches (
			snapshot_id            BIGINT NOT NULL REFERENCES wf5m_snapshots(id) ON DELETE CASCADE,
			address                TEXT NOT NULL,
			win_rate               DOUBLE PRECISION,
			crypto_ratio           DOUBLE PRECISION,
			dual_market_ratio      DOUBLE PRECISION,
			hold_to_settle_ratio   DOUBLE PRECISION,
			total_trades           INT,
			net_pnl_usd            DOUBLE PRECISION,
			market_win_rate_wilson DOUBLE PRECISION,
			no_reduce_ratio        DOUBLE PRECISION,
			settled_market_count   INT,
			price_band_roi_pct     DOUBLE PRECISION,
			PRIMARY KEY (snapshot_id, address)
		)`,
		`ALTER TABLE wf5m_snapshot_matches ADD COLUMN IF NOT EXISTS market_win_rate_wilson DOUBLE PRECISION`,
		`ALTER TABLE wf5m_snapshot_matches ADD COLUMN IF NOT EXISTS no_reduce_ratio DOUBLE PRECISION`,
		`ALTER TABLE wf5m_snapshot_matches ADD COLUMN IF NOT EXISTS settled_market_count INT`,
		`ALTER TABLE wf5m_snapshot_matches ADD COLUMN IF NOT EXISTS price_band_roi_pct DOUBLE PRECISION`,
		`CREATE INDEX IF NOT EXISTS idx_wf5m_snapshot_matches_addr ON wf5m_snapshot_matches(address)`,
	}
	for i, stmt := range stmts {
		if _, err := db.sql.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration step %d: %w", i, err)
		}
	}

	for _, c := range defaultConfig() {
		if _, err := db.sql.ExecContext(ctx,
			`INSERT INTO wf5m_config (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`,
			c.key, c.value); err != nil {
			return fmt.Errorf("seed config %s: %w", c.key, err)
		}
	}

	if err := db.MigrateCryptoFinder(ctx); err != nil {
		return fmt.Errorf("cryptofinder migration: %w", err)
	}
	log.Printf("[wf5m] migration ok: wf5m_* tables + crypto_wallet_candidates ready")
	return nil
}

type defaultCfg struct {
	key   string
	value float64
}

func defaultConfig() []defaultCfg {
	return []defaultCfg{
		{"dual_market_ratio_max", 25},
		{"crypto_ratio_min", 80},
		{"hold_to_settle_ratio_min", 80},
		{"min_trades", 30},
		{"settled_markets_min", 100},
		{"market_wilson_min", 52},
		{"no_reduce_ratio_min", 95},
		{"price_band_markets_min", 30},
		{"price_band_roi_min", 0},
	}
}
