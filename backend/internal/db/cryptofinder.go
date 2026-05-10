// cryptofinder.go — wf5m 對 crypto_wallet_candidates 的「寫入」職責。
//
// 這部分原本住在 poly-tracker/internal/db/db.go，搬過來後 poly-tracker 那邊
// 會被刪。table 本身用 IF NOT EXISTS migration，poly-tracker 過渡期還在的話
// 兩邊宣告 schema 完全相同（同欄位、同 type、同 default），不會打架。
//
// 寫入時機：scanner.Run() Phase 2 完成後一次 ReplaceCryptoCandidates 整批 DELETE+INSERT。
//
// candidates.go 的 Candidate (FindCandidates 用) 是「讀」的子集，這邊的 CryptoWalletCandidate
// 是「寫」的完整 struct。兩邊欄位對齊。

package db

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

// CryptoWalletCandidate 是 cryptofinder 評估後的一筆完整 wallet stats。
// 對應 poly-tracker 的 db.CryptoWalletCandidate（搬遷時保持 wire-format 相同）。
type CryptoWalletCandidate struct {
	Address     string
	TotalTrades int
	WinCount    int
	LoseCount   int
	WinRate     float64
	ROIPct      float64
	NetPnLUSD   float64
	VolumeUSD   float64
	LastTradeAt time.Time

	// 24h subset
	TotalTrades24h int
	WinCount24h    int
	LoseCount24h   int
	WinRate24h     float64
	ROIPct24h      float64
	NetPnLUSD24h   float64
	VolumeUSD24h   float64

	// 進階指標
	AvgWinUSD         float64
	AvgLossUSD        float64
	ProfitLossRatio   float64
	MaxDrawdownUSD    float64
	CryptoRatio       float64
	WashRatio         float64
	WashCount         int
	AvgBuyPrice       float64
	ExtremePriceRatio float64
	AvgBuyOffsetSec   float64
	DualMarketRatio   float64
	HoldToSettleRatio float64

	SettledMarketCount      int
	MarketWinCount          int
	MarketLossCount         int
	MarketWinRate           float64
	MarketWinRateWilson     float64
	NoReduceRatio           float64
	ReduceBeforeSettleRatio float64
	AddMarketRatio          float64
	AvgAddsPerMarket        float64
	PriceBandMarketCount    int
	PriceBandWinCount       int
	PriceBandLossCount      int
	PriceBandWinRate        float64
	PriceBandWinRateWilson  float64
	PriceBandROIPct         float64
	PriceBandNetPnLUSD      float64
	PriceBandVolumeUSD      float64
}

// MigrateCryptoFinder 建 crypto_wallet_candidates + crypto_finder_runs。
// IF NOT EXISTS / ALTER ADD COLUMN IF NOT EXISTS — 跟 poly-tracker 的 migration 並存安全。
//
// 等 poly-tracker 拔掉之後，這邊就是 schema 唯一 owner。
func (db *DB) MigrateCryptoFinder(ctx context.Context) error {
	if _, err := db.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS crypto_wallet_candidates (
			address           TEXT        PRIMARY KEY,
			total_trades      INT         NOT NULL DEFAULT 0,
			win_count         INT         NOT NULL DEFAULT 0,
			lose_count        INT         NOT NULL DEFAULT 0,
			win_rate          FLOAT       NOT NULL DEFAULT 0,
			roi_pct           FLOAT       NOT NULL DEFAULT 0,
			net_pnl_usd       FLOAT       NOT NULL DEFAULT 0,
			volume_usd        FLOAT       NOT NULL DEFAULT 0,
			last_trade_at     TIMESTAMPTZ,
			evaluated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create crypto_wallet_candidates: %w", err)
	}
	// migration 欄位（跟 poly-tracker 同步）
	for _, col := range []string{
		`total_trades_24h INT NOT NULL DEFAULT 0`,
		`win_count_24h INT NOT NULL DEFAULT 0`,
		`lose_count_24h INT NOT NULL DEFAULT 0`,
		`win_rate_24h FLOAT NOT NULL DEFAULT 0`,
		`roi_pct_24h FLOAT NOT NULL DEFAULT 0`,
		`net_pnl_usd_24h FLOAT NOT NULL DEFAULT 0`,
		`volume_usd_24h FLOAT NOT NULL DEFAULT 0`,
		`avg_win_usd FLOAT NOT NULL DEFAULT 0`,
		`avg_loss_usd FLOAT NOT NULL DEFAULT 0`,
		`profit_loss_ratio FLOAT NOT NULL DEFAULT 0`,
		`max_drawdown_usd FLOAT NOT NULL DEFAULT 0`,
		`crypto_ratio FLOAT NOT NULL DEFAULT 0`,
		`wash_ratio FLOAT NOT NULL DEFAULT 0`,
		`wash_count INT NOT NULL DEFAULT 0`,
		`avg_buy_price FLOAT NOT NULL DEFAULT 0`,
		`extreme_price_ratio FLOAT NOT NULL DEFAULT 0`,
		`avg_buy_offset_sec FLOAT NOT NULL DEFAULT 0`,
		`dual_market_ratio FLOAT NOT NULL DEFAULT 0`,
		`hold_to_settle_ratio FLOAT NOT NULL DEFAULT 0`,
		`settled_market_count INT NOT NULL DEFAULT 0`,
		`market_win_count INT NOT NULL DEFAULT 0`,
		`market_loss_count INT NOT NULL DEFAULT 0`,
		`market_win_rate FLOAT NOT NULL DEFAULT 0`,
		`market_win_rate_wilson FLOAT NOT NULL DEFAULT 0`,
		`no_reduce_ratio FLOAT NOT NULL DEFAULT 0`,
		`reduce_before_settle_ratio FLOAT NOT NULL DEFAULT 0`,
		`add_market_ratio FLOAT NOT NULL DEFAULT 0`,
		`avg_adds_per_market FLOAT NOT NULL DEFAULT 0`,
		`price_band_market_count INT NOT NULL DEFAULT 0`,
		`price_band_win_count INT NOT NULL DEFAULT 0`,
		`price_band_loss_count INT NOT NULL DEFAULT 0`,
		`price_band_win_rate FLOAT NOT NULL DEFAULT 0`,
		`price_band_win_rate_wilson FLOAT NOT NULL DEFAULT 0`,
		`price_band_roi_pct FLOAT NOT NULL DEFAULT 0`,
		`price_band_net_pnl_usd FLOAT NOT NULL DEFAULT 0`,
		`price_band_volume_usd FLOAT NOT NULL DEFAULT 0`,
	} {
		if _, err := db.sql.ExecContext(ctx,
			`ALTER TABLE crypto_wallet_candidates ADD COLUMN IF NOT EXISTS `+col); err != nil {
			return fmt.Errorf("add column %q: %w", col, err)
		}
	}
	if _, err := db.sql.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_crypto_candidates_pnl ON crypto_wallet_candidates (net_pnl_usd DESC)`); err != nil {
		return fmt.Errorf("index pnl: %w", err)
	}
	if _, err := db.sql.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_crypto_candidates_market_w95 ON crypto_wallet_candidates (market_win_rate_wilson DESC)`); err != nil {
		return fmt.Errorf("index market w95: %w", err)
	}
	if _, err := db.sql.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_crypto_candidates_price_band_roi ON crypto_wallet_candidates (price_band_roi_pct DESC)`); err != nil {
		return fmt.Errorf("index price band roi: %w", err)
	}
	if _, err := db.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS crypto_finder_runs (
			id              BIGSERIAL    PRIMARY KEY,
			started_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			finished_at     TIMESTAMPTZ,
			days_window     INT          NOT NULL,
			min_trades      INT          NOT NULL,
			seed_count      INT          NOT NULL DEFAULT 0,
			candidate_count INT          NOT NULL DEFAULT 0,
			error           TEXT         NOT NULL DEFAULT ''
		)
	`); err != nil {
		return fmt.Errorf("create crypto_finder_runs: %w", err)
	}
	return nil
}

// ReplaceCryptoCandidates 一次清掉舊的 + 寫入新的（atomic via tx）。
// 跟 poly-tracker 完全相同的 wire format — 過渡期兩邊輪流寫不會破壞。
func (db *DB) ReplaceCryptoCandidates(ctx context.Context, items []CryptoWalletCandidate) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM crypto_wallet_candidates`); err != nil {
		return fmt.Errorf("clear: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO crypto_wallet_candidates
		  (address, total_trades, win_count, lose_count, win_rate, roi_pct, net_pnl_usd, volume_usd, last_trade_at, evaluated_at,
		   total_trades_24h, win_count_24h, lose_count_24h, win_rate_24h, roi_pct_24h, net_pnl_usd_24h, volume_usd_24h,
		   avg_win_usd, avg_loss_usd, profit_loss_ratio, max_drawdown_usd, crypto_ratio, wash_ratio, wash_count,
		   avg_buy_price, extreme_price_ratio, avg_buy_offset_sec, dual_market_ratio, hold_to_settle_ratio,
		   settled_market_count, market_win_count, market_loss_count, market_win_rate, market_win_rate_wilson,
		   no_reduce_ratio, reduce_before_settle_ratio, add_market_ratio, avg_adds_per_market,
		   price_band_market_count, price_band_win_count, price_band_loss_count, price_band_win_rate,
		   price_band_win_rate_wilson, price_band_roi_pct, price_band_net_pnl_usd, price_band_volume_usd)
		VALUES (LOWER($1), $2, $3, $4, $5, $6, $7, $8, $9, NOW(),
		        $10, $11, $12, $13, $14, $15, $16,
		        $17, $18, $19, $20, $21, $22, $23,
		        $24, $25, $26, $27, $28,
		        $29, $30, $31, $32, $33,
		        $34, $35, $36, $37,
		        $38, $39, $40, $41,
		        $42, $43, $44, $45)
	`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, it := range items {
		var lt interface{}
		if !it.LastTradeAt.IsZero() {
			lt = it.LastTradeAt
		}
		if _, err := stmt.ExecContext(ctx,
			it.Address, it.TotalTrades, it.WinCount, it.LoseCount,
			it.WinRate, it.ROIPct, it.NetPnLUSD, it.VolumeUSD, lt,
			it.TotalTrades24h, it.WinCount24h, it.LoseCount24h,
			it.WinRate24h, it.ROIPct24h, it.NetPnLUSD24h, it.VolumeUSD24h,
			it.AvgWinUSD, it.AvgLossUSD, it.ProfitLossRatio, it.MaxDrawdownUSD,
			it.CryptoRatio, it.WashRatio, it.WashCount,
			it.AvgBuyPrice, it.ExtremePriceRatio, it.AvgBuyOffsetSec,
			it.DualMarketRatio, it.HoldToSettleRatio,
			it.SettledMarketCount, it.MarketWinCount, it.MarketLossCount,
			it.MarketWinRate, it.MarketWinRateWilson,
			it.NoReduceRatio, it.ReduceBeforeSettleRatio,
			it.AddMarketRatio, it.AvgAddsPerMarket,
			it.PriceBandMarketCount, it.PriceBandWinCount, it.PriceBandLossCount,
			it.PriceBandWinRate, it.PriceBandWinRateWilson,
			it.PriceBandROIPct, it.PriceBandNetPnLUSD, it.PriceBandVolumeUSD,
		); err != nil {
			return fmt.Errorf("insert %s: %w", it.Address, err)
		}
	}
	return tx.Commit()
}

// ── crypto_finder_runs (scan 歷史) ───────────────────────────────────────────

type CryptoFinderRun struct {
	ID             int64      `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	DaysWindow     int        `json:"days_window"`
	MinTrades      int        `json:"min_trades"`
	SeedCount      int        `json:"seed_count"`
	CandidateCount int        `json:"candidate_count"`
	Error          string     `json:"error,omitempty"`
}

func (db *DB) StartCryptoFinderRun(ctx context.Context, days, minTrades int) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `
		INSERT INTO crypto_finder_runs (days_window, min_trades) VALUES ($1, $2) RETURNING id
	`, days, minTrades).Scan(&id)
	return id, err
}

func (db *DB) FinishCryptoFinderRun(ctx context.Context, id int64, seedCount, candCount int, errMsg string) error {
	_, err := db.sql.ExecContext(ctx, `
		UPDATE crypto_finder_runs
		SET finished_at = NOW(), seed_count = $2, candidate_count = $3, error = $4
		WHERE id = $1
	`, id, seedCount, candCount, errMsg)
	return err
}

func (db *DB) LastCryptoFinderRun(ctx context.Context) (*CryptoFinderRun, error) {
	var r CryptoFinderRun
	var fin sql.NullTime
	err := db.sql.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, days_window, min_trades,
		       seed_count, candidate_count, error
		FROM crypto_finder_runs
		ORDER BY id DESC LIMIT 1
	`).Scan(&r.ID, &r.StartedAt, &fin, &r.DaysWindow, &r.MinTrades,
		&r.SeedCount, &r.CandidateCount, &r.Error)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if fin.Valid {
		r.FinishedAt = &fin.Time
	}
	return &r, nil
}

// WilsonLowerBound95 — 從 poly-tracker 搬過來的 helper。candidates.go 的 GetCandidates
// 之後若要回傳 wilson 下界（讓 wf5m 前端排序更準）可以用。
//
// 公式：(p̂ + z²/(2n) - z·sqrt(p̂(1-p̂)/n + z²/(4n²))) / (1 + z²/n) × 100
// z=1.96 = 95% 一邊置信區間。
func WilsonLowerBound95(wins, total int) float64 {
	if total <= 0 {
		return 0
	}
	n := float64(total)
	pHat := float64(wins) / n
	const z = 1.96
	const z2 = z * z
	denom := 1 + z2/n
	centre := pHat + z2/(2*n)
	margin := z * math.Sqrt(pHat*(1-pHat)/n+z2/(4*n*n))
	return (centre - margin) / denom * 100
}
