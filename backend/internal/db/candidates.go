package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Candidate 是前端候選錢包列表使用的投影。
type Candidate struct {
	Address           string  `json:"address"`
	TotalTrades       int     `json:"total_trades"`
	WinCount          int     `json:"win_count"`
	LoseCount         int     `json:"lose_count"`
	WinRate           float64 `json:"win_rate"`
	ROIPct            float64 `json:"roi_pct"`
	NetPnLUSD         float64 `json:"net_pnl_usd"`
	VolumeUSD         float64 `json:"volume_usd"`
	CryptoRatio       float64 `json:"crypto_ratio"`
	DualMarketRatio   float64 `json:"dual_market_ratio"`
	HoldToSettleRatio float64 `json:"hold_to_settle_ratio"`
	AvgBuyOffsetSec   float64 `json:"avg_buy_offset_sec"`
	ProfitLossRatio   float64 `json:"profit_loss_ratio"`
	WashRatio         float64 `json:"wash_ratio"`
	AvgBuyPrice       float64 `json:"avg_buy_price"`
	ExtremePriceRatio float64 `json:"extreme_price_ratio"`

	SettledMarketCount      int     `json:"settled_market_count"`
	MarketWinCount          int     `json:"market_win_count"`
	MarketLossCount         int     `json:"market_loss_count"`
	MarketWinRate           float64 `json:"market_win_rate"`
	MarketWinRateWilson     float64 `json:"market_win_rate_wilson"`
	NoReduceRatio           float64 `json:"no_reduce_ratio"`
	ReduceBeforeSettleRatio float64 `json:"reduce_before_settle_ratio"`
	AddMarketRatio          float64 `json:"add_market_ratio"`
	AvgAddsPerMarket        float64 `json:"avg_adds_per_market"`
	PriceBandMarketCount    int     `json:"price_band_market_count"`
	PriceBandWinCount       int     `json:"price_band_win_count"`
	PriceBandLossCount      int     `json:"price_band_loss_count"`
	PriceBandWinRate        float64 `json:"price_band_win_rate"`
	PriceBandWinRateWilson  float64 `json:"price_band_win_rate_wilson"`
	PriceBandROIPct         float64 `json:"price_band_roi_pct"`
	PriceBandNetPnLUSD      float64 `json:"price_band_net_pnl_usd"`
	PriceBandVolumeUSD      float64 `json:"price_band_volume_usd"`
	PriceBandMarketRatio    float64 `json:"price_band_market_ratio"`
	CopyableBucketCount     int     `json:"copyable_bucket_count"`
	CopyableMarketCount     int     `json:"copyable_market_count"`
	CopyableWinRate         float64 `json:"copyable_win_rate"`
	CopyableWinRateWilson   float64 `json:"copyable_win_rate_wilson"`
	CopyableROIPct          float64 `json:"copyable_roi_pct"`
	CopyableNetPnLUSD       float64 `json:"copyable_net_pnl_usd"`
	BestBucketLabel         string  `json:"best_bucket_label"`
	BestBucketMarketCount   int     `json:"best_bucket_market_count"`
	BestBucketWinRateWilson float64 `json:"best_bucket_win_rate_wilson"`
	BestBucketROIPct        float64 `json:"best_bucket_roi_pct"`
	BucketSummary           string  `json:"bucket_summary"`
	FirstBuyMarketCount     int     `json:"first_buy_market_count"`
	FirstBuyWinCount        int     `json:"first_buy_win_count"`
	FirstBuyLossCount       int     `json:"first_buy_loss_count"`
	FirstBuyWinRate         float64 `json:"first_buy_win_rate"`
	FirstBuyWinRateWilson   float64 `json:"first_buy_win_rate_wilson"`
	FirstBuyROIPct          float64 `json:"first_buy_roi_pct"`
	FirstBuyNetPnLUSD       float64 `json:"first_buy_net_pnl_usd"`
	FirstBuyVolumeUSD       float64 `json:"first_buy_volume_usd"`

	LastTradeAt     time.Time `json:"last_trade_at,omitempty"`
	EvaluatedAt     time.Time `json:"evaluated_at"`
	AlreadyObserved bool      `json:"already_observed"`
}

// CandidateFilter 是候選查詢的硬條件；零值代表不額外限制。
type CandidateFilter struct {
	DualMaxPct           float64
	CryptoMinPct         float64
	HoldMinPct           float64
	MinTrades            int
	SettledMarketsMin    int
	MarketW95MinPct      float64
	NoReduceMinPct       float64
	PriceBandMarketsMin  int
	PriceBandROIMinPct   float64
	AvgBuyPriceMax       float64
	ExtremePriceMaxPct   float64
	PriceBandRatioMinPct float64
	CopyableBucketsMin   int
	CopyableROIMinPct    float64
	FirstBuyMarketsMin   int
	FirstBuyROIMinPct    float64
	SortBy               string
	Limit                int
}

// FindCandidates 依照目前 filter 回傳候選錢包，並標記是否已加入 copy_sim_wallets。
func (db *DB) FindCandidates(ctx context.Context, f CandidateFilter) ([]Candidate, error) {
	priceBandRatioExpr := `CASE WHEN c.settled_market_count > 0 THEN c.price_band_market_count::float / c.settled_market_count * 100 ELSE 0 END`
	sortExpr := "c.net_pnl_usd"
	switch f.SortBy {
	case "win_rate", "roi_pct", "total_trades", "hold_to_settle_ratio",
		"market_win_rate", "market_win_rate_wilson", "settled_market_count",
		"no_reduce_ratio", "add_market_ratio", "price_band_market_count",
		"price_band_win_rate", "price_band_win_rate_wilson", "price_band_roi_pct",
		"avg_buy_price", "extreme_price_ratio", "copyable_bucket_count",
		"copyable_market_count", "copyable_win_rate", "copyable_win_rate_wilson",
		"copyable_roi_pct", "copyable_net_pnl_usd", "best_bucket_roi_pct",
		"first_buy_market_count", "first_buy_win_rate", "first_buy_win_rate_wilson",
		"first_buy_roi_pct", "first_buy_net_pnl_usd":
		sortExpr = "c." + f.SortBy
	case "price_band_market_ratio":
		sortExpr = priceBandRatioExpr
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	avgBuyPriceMax := f.AvgBuyPriceMax
	if avgBuyPriceMax <= 0 || avgBuyPriceMax > 1 {
		avgBuyPriceMax = 1
	}
	extremePriceMaxPct := f.ExtremePriceMaxPct
	if extremePriceMaxPct <= 0 || extremePriceMaxPct > 100 {
		extremePriceMaxPct = 100
	}

	q := fmt.Sprintf(`
		SELECT c.address, c.total_trades, c.win_count, c.lose_count,
		       c.win_rate, c.roi_pct, c.net_pnl_usd, c.volume_usd,
		       c.crypto_ratio, c.dual_market_ratio, c.hold_to_settle_ratio,
		       c.avg_buy_offset_sec, c.profit_loss_ratio, c.wash_ratio,
		       c.avg_buy_price, c.extreme_price_ratio,
		       c.settled_market_count, c.market_win_count, c.market_loss_count,
		       c.market_win_rate, c.market_win_rate_wilson,
		       c.no_reduce_ratio, c.reduce_before_settle_ratio,
		       c.add_market_ratio, c.avg_adds_per_market,
		       c.price_band_market_count, c.price_band_win_count, c.price_band_loss_count,
		       c.price_band_win_rate, c.price_band_win_rate_wilson,
		       c.price_band_roi_pct, c.price_band_net_pnl_usd, c.price_band_volume_usd,
		       %s AS price_band_market_ratio,
		       c.copyable_bucket_count, c.copyable_market_count,
		       c.copyable_win_rate, c.copyable_win_rate_wilson,
		       c.copyable_roi_pct, c.copyable_net_pnl_usd,
		       c.best_bucket_label, c.best_bucket_market_count,
		       c.best_bucket_win_rate_wilson, c.best_bucket_roi_pct,
		       c.bucket_summary,
		       c.first_buy_market_count, c.first_buy_win_count, c.first_buy_loss_count,
		       c.first_buy_win_rate, c.first_buy_win_rate_wilson,
		       c.first_buy_roi_pct, c.first_buy_net_pnl_usd, c.first_buy_volume_usd,
		       c.last_trade_at, c.evaluated_at,
		       (s.address IS NOT NULL) AS already_observed
		FROM crypto_wallet_candidates c
		LEFT JOIN copy_sim_wallets s ON LOWER(s.address) = c.address
		WHERE c.dual_market_ratio       <= $1
		  AND c.crypto_ratio            >= $2
		  AND c.hold_to_settle_ratio    >= $3
		  AND c.total_trades            >= $4
		  AND c.settled_market_count    >= $5
		  AND c.market_win_rate_wilson  >= $6
		  AND c.no_reduce_ratio         >= $7
		  AND c.price_band_market_count >= $8
		  AND c.price_band_roi_pct      >= $9
		  AND c.avg_buy_price           <= $10
		  AND c.extreme_price_ratio     <= $11
		  AND %s                        >= $12
		  AND c.copyable_bucket_count   >= $13
		  AND c.copyable_roi_pct        >= $14
		  AND c.first_buy_market_count  >= $15
		  AND c.first_buy_roi_pct       >= $16
		ORDER BY %s DESC
		LIMIT $17
	`, priceBandRatioExpr, priceBandRatioExpr, sortExpr)

	rows, err := db.sql.QueryContext(ctx, q,
		f.DualMaxPct, f.CryptoMinPct, f.HoldMinPct, f.MinTrades,
		f.SettledMarketsMin, f.MarketW95MinPct, f.NoReduceMinPct,
		f.PriceBandMarketsMin, f.PriceBandROIMinPct,
		avgBuyPriceMax, extremePriceMaxPct, f.PriceBandRatioMinPct,
		f.CopyableBucketsMin, f.CopyableROIMinPct,
		f.FirstBuyMarketsMin, f.FirstBuyROIMinPct, limit)
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
			&c.AvgBuyPrice, &c.ExtremePriceRatio,
			&c.SettledMarketCount, &c.MarketWinCount, &c.MarketLossCount,
			&c.MarketWinRate, &c.MarketWinRateWilson,
			&c.NoReduceRatio, &c.ReduceBeforeSettleRatio,
			&c.AddMarketRatio, &c.AvgAddsPerMarket,
			&c.PriceBandMarketCount, &c.PriceBandWinCount, &c.PriceBandLossCount,
			&c.PriceBandWinRate, &c.PriceBandWinRateWilson,
			&c.PriceBandROIPct, &c.PriceBandNetPnLUSD, &c.PriceBandVolumeUSD,
			&c.PriceBandMarketRatio,
			&c.CopyableBucketCount, &c.CopyableMarketCount,
			&c.CopyableWinRate, &c.CopyableWinRateWilson,
			&c.CopyableROIPct, &c.CopyableNetPnLUSD,
			&c.BestBucketLabel, &c.BestBucketMarketCount,
			&c.BestBucketWinRateWilson, &c.BestBucketROIPct,
			&c.BucketSummary,
			&c.FirstBuyMarketCount, &c.FirstBuyWinCount, &c.FirstBuyLossCount,
			&c.FirstBuyWinRate, &c.FirstBuyWinRateWilson,
			&c.FirstBuyROIPct, &c.FirstBuyNetPnLUSD, &c.FirstBuyVolumeUSD,
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

// CountCandidates 回傳目前候選池總數，不套 UI filter。
func (db *DB) CountCandidates(ctx context.Context) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM crypto_wallet_candidates`).Scan(&n)
	return n, err
}
