package db

import (
	"context"
	"fmt"
	"strings"
)

// AddCopySimWallet 把 wallet 寫入 poly-tracker 的 copy_sim_wallets 表。
// 同 schema、同 UPSERT 行為（poly-tracker/internal/db/copysim.go）：
//
//	INSERT INTO copy_sim_wallets (address, alias, auto_added)
//	VALUES (LOWER($1), $2, $3)
//	ON CONFLICT (address) DO UPDATE SET ... (保留既有 enabled / alias)
//
// auto_added=true 標明這筆是 finder 自動加入（vs 手動）。
// 注意：address 必須 0x + 40 hex；caller 自己驗證。
func (db *DB) AddCopySimWallet(ctx context.Context, address, alias string) error {
	address = strings.ToLower(strings.TrimSpace(address))
	if !strings.HasPrefix(address, "0x") || len(address) != 42 {
		return fmt.Errorf("invalid address: %s", address)
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO copy_sim_wallets (address, alias, auto_added)
		VALUES (LOWER($1), $2, TRUE)
		ON CONFLICT (address) DO UPDATE SET
		  alias = CASE WHEN copy_sim_wallets.alias = '' THEN EXCLUDED.alias ELSE copy_sim_wallets.alias END,
		  enabled = TRUE
	`, address, alias)
	return err
}
