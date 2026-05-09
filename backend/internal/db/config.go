package db

import (
	"context"
	"fmt"
	"time"
)

// ConfigEntry 是 wf5m_config 一筆。
type ConfigEntry struct {
	Key       string    `json:"key"`
	Value     float64   `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConfigChange 是 wf5m_config_history 一筆 audit。
type ConfigChange struct {
	ID        int64     `json:"id"`
	Key       string    `json:"key"`
	OldValue  *float64  `json:"old_value,omitempty"`
	NewValue  float64   `json:"new_value"`
	ChangedAt time.Time `json:"changed_at"`
}

// GetAllConfig 撈當前 wf5m_config 全部 entries 成 map[key]value。
func (db *DB) GetAllConfig(ctx context.Context) (map[string]float64, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT key, value FROM wf5m_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetConfig 更新一個 key 的 value，同步寫 audit log。
// 如果新舊 value 一樣，不寫 audit（避免雜訊）。
func (db *DB) SetConfig(ctx context.Context, key string, newValue float64) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldValue *float64
	var existing float64
	err = tx.QueryRowContext(ctx, `SELECT value FROM wf5m_config WHERE key = $1`, key).Scan(&existing)
	if err == nil {
		v := existing
		oldValue = &v
	}
	// 若值沒變,不更新不寫 audit
	if oldValue != nil && *oldValue == newValue {
		return tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wf5m_config (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, newValue); err != nil {
		return fmt.Errorf("upsert config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wf5m_config_history (key, old_value, new_value)
		VALUES ($1, $2, $3)
	`, key, oldValue, newValue); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return tx.Commit()
}

// GetConfigHistory 撈最近 N 筆 audit。
func (db *DB) GetConfigHistory(ctx context.Context, limit int) ([]ConfigChange, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, key, old_value, new_value, changed_at
		FROM wf5m_config_history
		ORDER BY changed_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfigChange
	for rows.Next() {
		var c ConfigChange
		if err := rows.Scan(&c.ID, &c.Key, &c.OldValue, &c.NewValue, &c.ChangedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
