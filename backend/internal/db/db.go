// Package db 是 wallets-finder-5mins 的 DB 層。
//
// 資料表分兩類：
//  1. 唯讀（poly-tracker 寫入）：crypto_wallet_candidates, copy_sim_wallets
//  2. 自有（wf5m_*）：filter config, audit log, scan snapshot
//
// 連同一個 poly_tracker DB；用 wf5m_* 前綴隔離 namespace 避免跟 poly-tracker 衝突。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	sql *sql.DB
}

func Open(dsn string) (*DB, error) {
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DB{sql: pool}, nil
}

func (db *DB) Close() error { return db.sql.Close() }

// SQL exposes the raw *sql.DB for callers that need it (avoid where possible).
func (db *DB) SQL() *sql.DB { return db.sql }
