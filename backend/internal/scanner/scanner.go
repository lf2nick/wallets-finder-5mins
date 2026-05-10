// Package scanner 是 cryptofinder 邏輯（從 poly-tracker/internal/cryptofinder 搬過來）。
//
// 為什麼搬：
//   - poly-tracker 的 cryptofinder 跟它的 trading bot 沒強耦合，純粹 wallet 發現
//   - wf5m 的職責就是「找錢包 + 篩 + 顯示」，把寫入端也納進來才是完整 owner
//   - poly-tracker 留 autofetch / autoremove / votefollow（這些跟 trading 才有關）
//
// 流程兩階段：
//  1. Seed 收集 (collectSeedWallets)：sample 最近 24h × 4 assets 的 5m 市場
//     對每個 slug 打 gamma /events 拿 conditionId → 打 data-api /trades 抓 wallet
//     並行 8 worker，預計 30-60s
//  2. 深度評估 (evaluateWallets)：對每個 seed 打 /activity，filter 5m crypto event
//     計算 W/L、ROI、PnL、雙向%、hold_to_settle% 等 18+ 指標
//     並行 6 worker，預計 3-7 min（取決於 seed 數量）
package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lf2nick/wallets-finder-5mins/backend/internal/db"
)

const (
	gammaAPI    = "https://gamma-api.polymarket.com"
	dataAPI     = "https://data-api.polymarket.com"
	intervalSec = 300 // 5 分鐘 = 300 秒（每個市場時間長度）

	// 並行控制（跟 poly-tracker 同設定）：太快被 rate-limit；太慢拉長 scan
	gammaConcurrency = 8
	dataConcurrency  = 6
	httpTimeout      = 15 * time.Second

	// Phase 2：每個 wallet 抓多少筆 activity（API 上限通常 500）
	activityLimit       = 500
	tradePageLimit      = 500
	tradePagesPerMarket = 3

	// 我們實際願意跟單的價格帶；市場級 EV 統計只看這段，避免高勝率被高買價吃掉。
	followPriceBandMin = 0.30
	followPriceBandMax = 0.70

	copyableBucketMinMarkets = 5
)

// 4 個資產的 slug 前綴
var assetPrefixes = []string{"btc-updown-5m", "eth-updown-5m", "sol-updown-5m", "xrp-updown-5m"}

// 市場結算結果快取（cross-wallet 共用 — 同 5m 市場很多 wallet 重複交易）。
// 為什麼需要：Polymarket /activity 的 REDEEM event 的 price 永遠 0（不填寫），
// 從 event 自己無法判斷輸贏 → 必須查 gamma 拿 outcomePrices 對照 BUY outcome。
type marketResolution struct {
	Resolved      bool
	OutcomePrices map[string]float64 // outcome name → payout per share at resolution
}

// Scanner 是主控制器（per-process 一個）。
//
// 設計：scan 由 API 觸發；同時只允許一個 scan 在跑（mu protected）。
// running=true 時再次 trigger 直接回 "already running"，前端可 poll status 看進度。
type Scanner struct {
	db         *db.DB
	httpClient *http.Client

	mu       sync.Mutex
	running  bool
	progress Progress

	// 結算 cache — instance scope。每次 New() 重置（避免長跑 process 累積無限 RAM）。
	resolutionCache sync.Map // map[eventSlug]*marketResolution
}

// Progress 給前端 poll 看當下狀態（避免靜默等 5 分鐘）。
type Progress struct {
	Phase            string `json:"phase"` // "idle" | "seeding" | "evaluating" | "done" | "error"
	Detail           string `json:"detail"`
	MarketsProbed    int    `json:"markets_probed"`
	MarketsTotal     int    `json:"markets_total"`
	WalletsEvaluated int    `json:"wallets_evaluated"`
	WalletsTotal     int    `json:"wallets_total"`
	StartedAt        int64  `json:"started_at,omitempty"` // unix ms
}

func New(database *db.DB) *Scanner {
	return &Scanner{
		db:         database,
		httpClient: &http.Client{Timeout: httpTimeout},
		progress:   Progress{Phase: "idle"},
	}
}

// IsRunning 給 API status 用。
func (s *Scanner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// SnapshotProgress 線程安全 copy。
func (s *Scanner) SnapshotProgress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// Scan 主入口；阻塞執行（caller 通常用 go 包起來）。
//
//	days: 評估窗口（filter activity timestamp >= now - days*24h）
//	minTrades: 寫入時的篩選（_目前 ReplaceCryptoCandidates 一律寫所有 wallet_，
//	  此參數只 propagate 進 crypto_finder_runs 紀錄）
//
// 回傳 (seedCount, candidateCount, error)。
func (s *Scanner) Scan(ctx context.Context, seedDays, evalDays, minTrades int) (int, int, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return 0, 0, fmt.Errorf("scan already running")
	}
	s.running = true
	s.progress = Progress{Phase: "seeding", StartedAt: time.Now().UnixMilli()}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	runID, err := s.db.StartCryptoFinderRun(ctx, seedDays, evalDays, minTrades)
	if err != nil {
		return 0, 0, fmt.Errorf("start run: %w", err)
	}

	seeds, err := s.collectSeedWallets(ctx, seedDays)
	if err != nil {
		_ = s.db.FinishCryptoFinderRun(ctx, runID, 0, 0, err.Error())
		s.setPhase("error", err.Error())
		return 0, 0, err
	}

	s.setProgress(Progress{Phase: "evaluating", WalletsTotal: len(seeds), StartedAt: s.progress.StartedAt})
	candidates := s.evaluateWallets(ctx, seeds, evalDays)

	if err := s.db.ReplaceCryptoCandidates(ctx, candidates); err != nil {
		_ = s.db.FinishCryptoFinderRun(ctx, runID, len(seeds), len(candidates), err.Error())
		return len(seeds), 0, fmt.Errorf("save candidates: %w", err)
	}

	if err := s.db.FinishCryptoFinderRun(ctx, runID, len(seeds), len(candidates), ""); err != nil {
		log.Printf("[scanner] 寫 finish run 失敗（不影響結果）: %v", err)
	}
	s.setPhase("done", fmt.Sprintf("seed=%d 候選=%d", len(seeds), len(candidates)))
	log.Printf("[scanner] ✅ Scan 完成: seed=%d 候選=%d", len(seeds), len(candidates))
	return len(seeds), len(candidates), nil
}

// ── Phase 1: seed collection ─────────────────────────────────────────────────

func (s *Scanner) collectSeedWallets(ctx context.Context, seedDays int) (map[string]struct{}, error) {
	now := time.Now().Unix()
	startTs := now - int64(seedDays)*86400
	startTs = (startTs / intervalSec) * intervalSec

	type job struct{ slug string }
	var jobs []job
	for ts := startTs; ts <= now; ts += intervalSec {
		for _, asset := range assetPrefixes {
			jobs = append(jobs, job{slug: fmt.Sprintf("%s-%d", asset, ts)})
		}
	}

	s.setProgress(Progress{Phase: "seeding", MarketsTotal: len(jobs), StartedAt: s.progress.StartedAt})
	log.Printf("[scanner] Phase 1: probe %d markets (4 assets × %dd × 12/h)", len(jobs), seedDays)

	jobCh := make(chan job, len(jobs))
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	seeds := make(map[string]struct{})
	var seedsMu sync.Mutex
	var probed int
	var probedMu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < gammaConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				if ctx.Err() != nil {
					return
				}
				wallets := s.fetchMarketWallets(ctx, j.slug)
				if len(wallets) > 0 {
					seedsMu.Lock()
					for _, w := range wallets {
						seeds[strings.ToLower(w)] = struct{}{}
					}
					seedsMu.Unlock()
				}
				probedMu.Lock()
				probed++
				p := probed
				probedMu.Unlock()
				if p%50 == 0 {
					s.setProgress(Progress{
						Phase:         "seeding",
						MarketsProbed: p,
						MarketsTotal:  len(jobs),
						Detail:        fmt.Sprintf("已掃 %d/%d 市場，發現 %d 個 wallet", p, len(jobs), len(seeds)),
						StartedAt:     s.progress.StartedAt,
					})
				}
			}
		}()
	}
	wg.Wait()

	log.Printf("[scanner] Phase 1 done: probed=%d markets, seeds=%d wallets", probed, len(seeds))
	return seeds, nil
}

func (s *Scanner) fetchMarketWallets(ctx context.Context, slug string) []string {
	url := fmt.Sprintf("%s/events?slug=%s", gammaAPI, slug)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var events []struct {
		Volume  float64 `json:"volume"`
		Markets []struct {
			ConditionID string `json:"conditionId"`
		} `json:"markets"`
	}
	if err := json.Unmarshal(body, &events); err != nil || len(events) == 0 {
		return nil
	}
	if len(events[0].Markets) == 0 {
		return nil
	}
	if events[0].Volume <= 0 {
		return nil
	}
	conditionID := events[0].Markets[0].ConditionID

	out := make([]string, 0, tradePageLimit)
	for page := 0; page < tradePagesPerMarket; page++ {
		offset := page * tradePageLimit
		url2 := fmt.Sprintf("%s/trades?market=%s&limit=%d&offset=%d", dataAPI, conditionID, tradePageLimit, offset)
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, url2, nil)
		resp2, err := s.httpClient.Do(req2)
		if err != nil {
			break
		}
		body2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		if resp2.StatusCode != 200 {
			break
		}
		var trades []struct {
			ProxyWallet string `json:"proxyWallet"`
		}
		if err := json.Unmarshal(body2, &trades); err != nil {
			break
		}
		for _, t := range trades {
			if t.ProxyWallet != "" {
				out = append(out, t.ProxyWallet)
			}
		}
		if len(trades) < tradePageLimit {
			break
		}
	}
	return out
}

// lookupResolution — 結算結果 cache。
// 失敗（或 not resolved）回 nil；caller 應跳過此 conditionId（無法判斷輸贏）。
func (s *Scanner) lookupResolution(ctx context.Context, eventSlug string) *marketResolution {
	if v, ok := s.resolutionCache.Load(eventSlug); ok {
		return v.(*marketResolution)
	}
	url := fmt.Sprintf("%s/events?slug=%s", gammaAPI, eventSlug)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var events []struct {
		Markets []struct {
			Closed        bool   `json:"closed"`
			Outcomes      string `json:"outcomes"`      // JSON-encoded "[\"Up\",\"Down\"]"
			OutcomePrices string `json:"outcomePrices"` // JSON-encoded "[\"0\",\"1\"]"
		} `json:"markets"`
	}
	if err := json.Unmarshal(body, &events); err != nil || len(events) == 0 || len(events[0].Markets) == 0 {
		return nil
	}
	m := events[0].Markets[0]
	if !m.Closed {
		return nil
	}
	var outcomes, prices []string
	if json.Unmarshal([]byte(m.Outcomes), &outcomes) != nil || json.Unmarshal([]byte(m.OutcomePrices), &prices) != nil {
		return nil
	}
	if len(outcomes) != len(prices) {
		return nil
	}
	res := &marketResolution{Resolved: true, OutcomePrices: make(map[string]float64, len(outcomes))}
	for i, o := range outcomes {
		var p float64
		fmt.Sscanf(prices[i], "%f", &p)
		res.OutcomePrices[o] = p
	}
	s.resolutionCache.Store(eventSlug, res)
	return res
}

// ── Phase 2: per-wallet 評估 ──────────────────────────────────────────────────

type activityItem struct {
	Type        string  `json:"type"`     // "TRADE" | "REDEEM" | "SPLIT" | "MERGE" | "CONVERSION"
	Side        string  `json:"side"`     // "BUY" | "SELL" (TRADE only)
	UsdcSize    float64 `json:"usdcSize"` // TRADE: 花的 USDC; SPLIT: deposit; REDEEM: 不可信
	Size        float64 `json:"size"`     // shares 數量（給 BUY 用）
	Price       float64 `json:"price"`    // BUY 的 outcome token 單價
	ConditionID string  `json:"conditionId"`
	EventSlug   string  `json:"eventSlug"`
	Outcome     string  `json:"outcome"`
	Timestamp   int64   `json:"timestamp"`
}

type priceBucketDef struct {
	Label string
	Min   float64
	Max   float64
}

type priceBucketStats struct {
	Label       string  `json:"label"`
	MarketCount int     `json:"market_count"`
	WinCount    int     `json:"win_count"`
	LossCount   int     `json:"loss_count"`
	WinRate     float64 `json:"win_rate"`
	Wilson      float64 `json:"wilson"`
	ROIPct      float64 `json:"roi_pct"`
	NetPnLUSD   float64 `json:"net_pnl_usd"`
	VolumeUSD   float64 `json:"volume_usd"`
	Copyable    bool    `json:"copyable"`
}

var copyablePriceBuckets = []priceBucketDef{
	{Label: "0.30-0.40", Min: 0.30, Max: 0.40},
	{Label: "0.40-0.50", Min: 0.40, Max: 0.50},
	{Label: "0.50-0.56", Min: 0.50, Max: 0.56},
	{Label: "0.56-0.60", Min: 0.56, Max: 0.60},
	{Label: "0.60-0.65", Min: 0.60, Max: 0.65},
	{Label: "0.65-0.70", Min: 0.65, Max: 0.70},
}

func bucketForPrice(price float64) string {
	for i, b := range copyablePriceBuckets {
		if price < b.Min || price > b.Max {
			continue
		}
		if price < b.Max || i == len(copyablePriceBuckets)-1 {
			return b.Label
		}
	}
	return ""
}

func (s *Scanner) evaluateWallets(ctx context.Context, seeds map[string]struct{}, days int) []db.CryptoWalletCandidate {
	addrs := make([]string, 0, len(seeds))
	for a := range seeds {
		addrs = append(addrs, a)
	}

	type job struct{ addr string }
	jobCh := make(chan job, len(addrs))
	for _, a := range addrs {
		jobCh <- job{addr: a}
	}
	close(jobCh)

	candidates := make([]db.CryptoWalletCandidate, 0, len(addrs))
	var candMu sync.Mutex
	var done int
	var doneMu sync.Mutex

	cutoff := time.Now().Unix() - int64(days)*86400

	var wg sync.WaitGroup
	for i := 0; i < dataConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				if ctx.Err() != nil {
					return
				}
				cand, ok := s.evaluateOneWallet(ctx, j.addr, cutoff)
				if ok {
					candMu.Lock()
					candidates = append(candidates, cand)
					candMu.Unlock()
				}
				doneMu.Lock()
				done++
				d := done
				doneMu.Unlock()
				if d%25 == 0 {
					s.setProgress(Progress{
						Phase:            "evaluating",
						WalletsEvaluated: d,
						WalletsTotal:     len(addrs),
						Detail:           fmt.Sprintf("評估 %d/%d 錢包...", d, len(addrs)),
						StartedAt:        s.progress.StartedAt,
					})
				}
			}
		}()
	}
	wg.Wait()

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].NetPnLUSD > candidates[j].NetPnLUSD
	})
	log.Printf("[scanner] Phase 2 done: 評估 %d 錢包 → %d 候選有 5m crypto trade", done, len(candidates))
	return candidates
}

// evaluateOneWallet — 算單一 wallet 的 18+ 指標。
// ok=false 表示完全沒這類 trade（不收）。
func (s *Scanner) evaluateOneWallet(ctx context.Context, addr string, cutoffTs int64) (db.CryptoWalletCandidate, bool) {
	url := fmt.Sprintf("%s/activity?user=%s&limit=%d&sortBy=timestamp&sortDirection=DESC", dataAPI, addr, activityLimit)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return db.CryptoWalletCandidate{}, false
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return db.CryptoWalletCandidate{}, false
	}
	var acts []activityItem
	if err := json.Unmarshal(body, &acts); err != nil {
		return db.CryptoWalletCandidate{}, false
	}

	// per-conditionId 累計：分 outcome 追蹤 BUY shares + 成本
	// hold_to_settle 判定用 sellShares + lastSellTs（搬過來的新邏輯）
	type pos struct {
		eventSlug          string
		buyShares          map[string]float64
		buyUSDC            float64
		buyTradeCount      int
		buyPriceSum        float64
		buyPriceShares     float64
		hasSell            bool
		closeTs            int64
		sellShares         map[string]float64
		lastSellTs         int64
		preCloseSellShares float64
		// 24h subset
		buyShares24h     map[string]float64
		buyUSDC24h       float64
		buyTradeCount24h int
		firstBuySet      bool
		firstBuyTs       int64
		firstBuyOutcome  string
		firstBuyPrice    float64
		firstBuySize     float64
		firstBuyUSDC     float64
	}
	positions := map[string]*pos{}

	cutoff24h := time.Now().Unix() - 86400

	var (
		totalTrades    int
		totalBuyUSDC   float64
		totalSellUSDC  float64
		totalSplitUSDC float64
		totalMergeUSDC float64
		totalConvUSDC  float64
		splitCount     int
		lastTradeTs    int64
		// 24h subset
		totalTrades24h    int
		totalBuyUSDC24h   float64
		totalSellUSDC24h  float64
		totalSplitUSDC24h float64
		totalMergeUSDC24h float64
		totalConvUSDC24h  float64
		// avg_buy_price + extreme_ratio
		buyPriceSumWeighted float64
		buySharesTotal      float64
		extremeBuyCount     int
		// avg_buy_offset_sec
		buyOffsetSumSec float64
		buyOffsetCount  int
	)
	_ = splitCount // 保留 var 與 poly-tracker 對齊（可能將來用）

	// 算「全部 activities 在窗口內」總數，給 crypto_ratio 用
	var totalActsInWindow int
	for _, a := range acts {
		if a.Timestamp >= cutoffTs {
			totalActsInWindow++
		}
	}

	for _, a := range acts {
		if a.Timestamp < cutoffTs {
			continue
		}
		// eventSlug 必須是 5m crypto 之一
		match := false
		for _, prefix := range assetPrefixes {
			if strings.HasPrefix(a.EventSlug, prefix+"-") {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		is24h := a.Timestamp >= cutoff24h

		p, ok := positions[a.ConditionID]
		if !ok {
			p = &pos{
				eventSlug:    a.EventSlug,
				buyShares:    make(map[string]float64),
				sellShares:   make(map[string]float64),
				buyShares24h: make(map[string]float64),
				closeTs:      parseCloseTs(a.EventSlug),
			}
			positions[a.ConditionID] = p
		}
		switch a.Type {
		case "TRADE":
			if a.Side == "BUY" {
				if a.Outcome != "" {
					p.buyShares[a.Outcome] += a.Size
					if is24h {
						p.buyShares24h[a.Outcome] += a.Size
					}
				}
				p.buyUSDC += a.UsdcSize
				p.buyTradeCount++
				totalBuyUSDC += a.UsdcSize
				totalTrades++
				if a.Timestamp > lastTradeTs {
					lastTradeTs = a.Timestamp
				}
				if is24h {
					p.buyUSDC24h += a.UsdcSize
					p.buyTradeCount24h++
					totalBuyUSDC24h += a.UsdcSize
					totalTrades24h++
				}
				if a.Price > 0 && a.Size > 0 {
					buyPriceSumWeighted += a.Price * a.Size
					buySharesTotal += a.Size
					p.buyPriceSum += a.Price * a.Size
					p.buyPriceShares += a.Size
					if a.Price >= 0.85 || a.Price <= 0.15 {
						extremeBuyCount++
					}
					if !p.firstBuySet || a.Timestamp < p.firstBuyTs {
						p.firstBuySet = true
						p.firstBuyTs = a.Timestamp
						p.firstBuyOutcome = a.Outcome
						p.firstBuyPrice = a.Price
						p.firstBuySize = a.Size
						p.firstBuyUSDC = a.UsdcSize
					}
				}
				if p.closeTs > 0 {
					marketStart := p.closeTs - 300
					offset := a.Timestamp - marketStart
					if offset >= 0 && offset <= 600 {
						buyOffsetSumSec += float64(offset)
						buyOffsetCount++
					}
				}
			} else if a.Side == "SELL" {
				p.hasSell = true
				if a.Outcome != "" {
					p.sellShares[a.Outcome] += a.Size
				}
				if a.Timestamp > p.lastSellTs {
					p.lastSellTs = a.Timestamp
				}
				if p.closeTs > 0 && a.Timestamp < p.closeTs {
					p.preCloseSellShares += a.Size
				}
				totalSellUSDC += a.UsdcSize
				if is24h {
					totalSellUSDC24h += a.UsdcSize
				}
			}
		case "REDEEM":
			// 不用 REDEEM 判定結算；market resolution lookup 才是正解
		case "SPLIT":
			totalSplitUSDC += a.UsdcSize
			splitCount++
			if is24h {
				totalSplitUSDC24h += a.UsdcSize
			}
		case "MERGE":
			totalMergeUSDC += a.UsdcSize
			if is24h {
				totalMergeUSDC24h += a.UsdcSize
			}
		case "CONVERSION":
			totalConvUSDC += a.UsdcSize
			if is24h {
				totalConvUSDC24h += a.UsdcSize
			}
		}
	}

	if totalTrades == 0 {
		return db.CryptoWalletCandidate{}, false
	}

	// (A) Arbitrage 指紋：SPLIT $ > BUY $ × 5 = MM/套利，跳過
	if totalSplitUSDC > totalBuyUSDC*5 {
		ratio := -1.0
		if totalBuyUSDC > 0 {
			ratio = totalSplitUSDC / totalBuyUSDC
		}
		log.Printf("[scanner] %.10s skip: SPLIT-arbitrage（split=$%.0f / buy=$%.0f, ratio=%.0fx）",
			addr, totalSplitUSDC, totalBuyUSDC, ratio)
		return db.CryptoWalletCandidate{}, false
	}

	// (B) 真實輸贏判斷
	type closedPos struct {
		closeTs int64
		netPnL  float64
	}
	var winCount, loseCount int
	var totalRedeemUSDC float64
	var winCount24h, loseCount24h int
	var totalRedeemUSDC24h float64
	var sumWinUSD, sumLossUSD float64
	var winMarkets, loseMarkets int
	var washCount int
	var settledMarketCount int
	var holdToSettleCount int
	var marketWinCount, marketLossCount int
	var noReduceCount, reduceBeforeSettleCount int
	var priceBandMarketCount, priceBandWinCount, priceBandLossCount int
	var priceBandVolumeUSD, priceBandNetPnLUSD float64
	var firstBuyMarketCount, firstBuyWinCount, firstBuyLossCount int
	var firstBuyVolumeUSD, firstBuyNetPnLUSD float64
	bucketStats := make(map[string]*priceBucketStats)
	closedList := make([]closedPos, 0)

	for _, p := range positions {
		if p.buyUSDC <= 0 {
			continue
		}
		res := s.lookupResolution(ctx, p.eventSlug)
		if res == nil || !res.Resolved {
			continue
		}
		settledMarketCount++
		if p.hasSell {
			washCount++
		}
		// hold_to_settle 判定（同 poly-tracker 的 0.99 fuzz threshold）
		var totalBuyShares, totalSellShares float64
		for _, sh := range p.buyShares {
			totalBuyShares += sh
		}
		for _, sh := range p.sellShares {
			totalSellShares += sh
		}
		exitBeforeSettle := totalBuyShares > 0 &&
			totalSellShares >= totalBuyShares*0.99 &&
			p.lastSellTs > 0 && p.closeTs > 0 &&
			p.lastSellTs < p.closeTs
		if !exitBeforeSettle {
			holdToSettleCount++
		}

		var posIncome float64
		for outcome, shares := range p.buyShares {
			if price, ok := res.OutcomePrices[outcome]; ok {
				posIncome += shares * price
			}
		}
		totalRedeemUSDC += posIncome
		netPnL := posIncome - p.buyUSDC
		closedList = append(closedList, closedPos{closeTs: p.closeTs, netPnL: netPnL})
		marketWon := posIncome >= p.buyUSDC
		if marketWon {
			marketWinCount++
			winCount += p.buyTradeCount
			winMarkets++
			sumWinUSD += netPnL
		} else {
			marketLossCount++
			loseCount += p.buyTradeCount
			loseMarkets++
			sumLossUSD += -netPnL
		}
		if p.preCloseSellShares > 0 {
			reduceBeforeSettleCount++
		} else {
			noReduceCount++
		}
		if p.buyPriceShares > 0 {
			avgMarketPrice := p.buyPriceSum / p.buyPriceShares
			if avgMarketPrice >= followPriceBandMin && avgMarketPrice <= followPriceBandMax {
				priceBandMarketCount++
				priceBandVolumeUSD += p.buyUSDC
				priceBandNetPnLUSD += netPnL
				if marketWon {
					priceBandWinCount++
				} else {
					priceBandLossCount++
				}
				if label := bucketForPrice(avgMarketPrice); label != "" {
					bs := bucketStats[label]
					if bs == nil {
						bs = &priceBucketStats{Label: label}
						bucketStats[label] = bs
					}
					bs.MarketCount++
					bs.VolumeUSD += p.buyUSDC
					bs.NetPnLUSD += netPnL
					if marketWon {
						bs.WinCount++
					} else {
						bs.LossCount++
					}
				}
			}
		}
		if p.firstBuySet && p.firstBuyPrice >= followPriceBandMin && p.firstBuyPrice <= followPriceBandMax {
			if price, ok := res.OutcomePrices[p.firstBuyOutcome]; ok {
				firstBuyIncome := p.firstBuySize * price
				firstBuyPnL := firstBuyIncome - p.firstBuyUSDC
				firstBuyMarketCount++
				firstBuyVolumeUSD += p.firstBuyUSDC
				firstBuyNetPnLUSD += firstBuyPnL
				if firstBuyIncome >= p.firstBuyUSDC {
					firstBuyWinCount++
				} else {
					firstBuyLossCount++
				}
			}
		}
		if p.buyUSDC24h > 0 {
			var posIncome24h float64
			for outcome, shares := range p.buyShares24h {
				if price, ok := res.OutcomePrices[outcome]; ok {
					posIncome24h += shares * price
				}
			}
			totalRedeemUSDC24h += posIncome24h
			if posIncome24h >= p.buyUSDC24h {
				winCount24h += p.buyTradeCount24h
			} else {
				loseCount24h += p.buyTradeCount24h
			}
		}
	}

	// 盈虧比
	avgWinUSD, avgLossUSD := 0.0, 0.0
	if winMarkets > 0 {
		avgWinUSD = sumWinUSD / float64(winMarkets)
	}
	if loseMarkets > 0 {
		avgLossUSD = sumLossUSD / float64(loseMarkets)
	}
	plRatio := 0.0
	if avgLossUSD > 0 {
		plRatio = avgWinUSD / avgLossUSD
	}

	// 最大回撤
	sort.Slice(closedList, func(i, j int) bool { return closedList[i].closeTs < closedList[j].closeTs })
	var cum, peak, maxDD float64
	for _, c := range closedList {
		cum += c.netPnL
		if cum > peak {
			peak = cum
		}
		if dd := peak - cum; dd > maxDD {
			maxDD = dd
		}
	}

	// 專攻度
	cryptoRatio := 0.0
	if totalActsInWindow > 0 {
		cryptoRatio = float64(totalTrades) / float64(totalActsInWindow) * 100
	}

	// 洗量比
	washRatio := 0.0
	if settledMarketCount > 0 {
		washRatio = float64(washCount) / float64(settledMarketCount) * 100
	}

	// hold_to_settle 比
	holdToSettleRatio := 0.0
	if settledMarketCount > 0 {
		holdToSettleRatio = float64(holdToSettleCount) / float64(settledMarketCount) * 100
	}

	// 雙向%
	dualMarketCount := 0
	allMarketCount := 0
	addMarketCount := 0
	totalAdds := 0
	for _, p := range positions {
		if p.buyUSDC <= 0 {
			continue
		}
		allMarketCount++
		if p.buyShares["Up"] > 0 && p.buyShares["Down"] > 0 {
			dualMarketCount++
		}
		if p.buyTradeCount > 1 {
			addMarketCount++
			totalAdds += p.buyTradeCount - 1
		}
	}
	dualMarketRatio := 0.0
	if allMarketCount > 0 {
		dualMarketRatio = float64(dualMarketCount) / float64(allMarketCount) * 100
	}
	addMarketRatio := 0.0
	avgAddsPerMarket := 0.0
	if allMarketCount > 0 {
		addMarketRatio = float64(addMarketCount) / float64(allMarketCount) * 100
		avgAddsPerMarket = float64(totalAdds) / float64(allMarketCount)
	}

	avgBuyPrice := 0.0
	if buySharesTotal > 0 {
		avgBuyPrice = buyPriceSumWeighted / buySharesTotal
	}
	extremePriceRatio := 0.0
	if totalTrades > 0 {
		extremePriceRatio = float64(extremeBuyCount) / float64(totalTrades) * 100
	}
	avgBuyOffsetSec := 0.0
	if buyOffsetCount > 0 {
		avgBuyOffsetSec = buyOffsetSumSec / float64(buyOffsetCount)
	}

	winRate := 0.0
	if settled := winCount + loseCount; settled > 0 {
		winRate = float64(winCount) / float64(settled) * 100
	}
	winRate24h := 0.0
	if settled := winCount24h + loseCount24h; settled > 0 {
		winRate24h = float64(winCount24h) / float64(settled) * 100
	}

	// (C) NetPnL — 完整公式（cost = BUY + SPLIT + CONV; income = REDEEM + MERGE + SELL）
	totalCost := totalBuyUSDC + totalSplitUSDC + totalConvUSDC
	marketWinRate := 0.0
	if settled := marketWinCount + marketLossCount; settled > 0 {
		marketWinRate = float64(marketWinCount) / float64(settled) * 100
	}
	marketWinRateWilson := db.WilsonLowerBound95(marketWinCount, marketWinCount+marketLossCount)
	noReduceRatio := 0.0
	reduceBeforeSettleRatio := 0.0
	if settledMarketCount > 0 {
		noReduceRatio = float64(noReduceCount) / float64(settledMarketCount) * 100
		reduceBeforeSettleRatio = float64(reduceBeforeSettleCount) / float64(settledMarketCount) * 100
	}
	priceBandWinRate := 0.0
	if settled := priceBandWinCount + priceBandLossCount; settled > 0 {
		priceBandWinRate = float64(priceBandWinCount) / float64(settled) * 100
	}
	priceBandWinRateWilson := db.WilsonLowerBound95(priceBandWinCount, priceBandWinCount+priceBandLossCount)
	priceBandROIPct := 0.0
	if priceBandVolumeUSD > 0 {
		priceBandROIPct = priceBandNetPnLUSD / priceBandVolumeUSD * 100
	}
	var copyableBucketCount, copyableMarketCount, copyableWinCount, copyableLossCount int
	var copyableVolumeUSD, copyableNetPnLUSD float64
	bestBucketLabel := ""
	bestBucketMarketCount := 0
	bestBucketWinRateWilson := 0.0
	bestBucketROIPct := 0.0
	bucketSummary := make([]priceBucketStats, 0, len(copyablePriceBuckets))
	for _, def := range copyablePriceBuckets {
		bs := bucketStats[def.Label]
		if bs == nil || bs.MarketCount == 0 {
			continue
		}
		settled := bs.WinCount + bs.LossCount
		if settled > 0 {
			bs.WinRate = float64(bs.WinCount) / float64(settled) * 100
			bs.Wilson = db.WilsonLowerBound95(bs.WinCount, settled)
		}
		if bs.VolumeUSD > 0 {
			bs.ROIPct = bs.NetPnLUSD / bs.VolumeUSD * 100
		}
		bs.Copyable = bs.MarketCount >= copyableBucketMinMarkets && bs.ROIPct > 0 && bs.NetPnLUSD > 0
		if bs.Copyable {
			copyableBucketCount++
			copyableMarketCount += bs.MarketCount
			copyableWinCount += bs.WinCount
			copyableLossCount += bs.LossCount
			copyableVolumeUSD += bs.VolumeUSD
			copyableNetPnLUSD += bs.NetPnLUSD
			if bestBucketLabel == "" || bs.ROIPct > bestBucketROIPct {
				bestBucketLabel = bs.Label
				bestBucketMarketCount = bs.MarketCount
				bestBucketWinRateWilson = bs.Wilson
				bestBucketROIPct = bs.ROIPct
			}
		}
		bucketSummary = append(bucketSummary, *bs)
	}
	copyableWinRate := 0.0
	if settled := copyableWinCount + copyableLossCount; settled > 0 {
		copyableWinRate = float64(copyableWinCount) / float64(settled) * 100
	}
	copyableWinRateWilson := db.WilsonLowerBound95(copyableWinCount, copyableWinCount+copyableLossCount)
	copyableROIPct := 0.0
	if copyableVolumeUSD > 0 {
		copyableROIPct = copyableNetPnLUSD / copyableVolumeUSD * 100
	}
	firstBuyWinRate := 0.0
	if settled := firstBuyWinCount + firstBuyLossCount; settled > 0 {
		firstBuyWinRate = float64(firstBuyWinCount) / float64(settled) * 100
	}
	firstBuyWinRateWilson := db.WilsonLowerBound95(firstBuyWinCount, firstBuyWinCount+firstBuyLossCount)
	firstBuyROIPct := 0.0
	if firstBuyVolumeUSD > 0 {
		firstBuyROIPct = firstBuyNetPnLUSD / firstBuyVolumeUSD * 100
	}
	bucketSummaryJSON := ""
	if len(bucketSummary) > 0 {
		if b, err := json.Marshal(bucketSummary); err == nil {
			bucketSummaryJSON = string(b)
		}
	}

	totalIncome := totalRedeemUSDC + totalMergeUSDC + totalSellUSDC
	netPnL := totalIncome - totalCost
	roi := 0.0
	if totalCost > 0 {
		roi = netPnL / totalCost * 100
	}

	totalCost24h := totalBuyUSDC24h + totalSplitUSDC24h + totalConvUSDC24h
	totalIncome24h := totalRedeemUSDC24h + totalMergeUSDC24h + totalSellUSDC24h
	netPnL24h := totalIncome24h - totalCost24h
	roi24h := 0.0
	if totalCost24h > 0 {
		roi24h = netPnL24h / totalCost24h * 100
	}

	return db.CryptoWalletCandidate{
		Address:     addr,
		TotalTrades: totalTrades,
		WinCount:    winCount,
		LoseCount:   loseCount,
		WinRate:     winRate,
		ROIPct:      roi,
		NetPnLUSD:   netPnL,
		VolumeUSD:   totalCost,
		LastTradeAt: time.Unix(lastTradeTs, 0).UTC(),

		TotalTrades24h: totalTrades24h,
		WinCount24h:    winCount24h,
		LoseCount24h:   loseCount24h,
		WinRate24h:     winRate24h,
		ROIPct24h:      roi24h,
		NetPnLUSD24h:   netPnL24h,
		VolumeUSD24h:   totalCost24h,

		AvgWinUSD:               avgWinUSD,
		AvgLossUSD:              avgLossUSD,
		ProfitLossRatio:         plRatio,
		MaxDrawdownUSD:          maxDD,
		CryptoRatio:             cryptoRatio,
		WashRatio:               washRatio,
		WashCount:               washCount,
		AvgBuyPrice:             avgBuyPrice,
		ExtremePriceRatio:       extremePriceRatio,
		AvgBuyOffsetSec:         avgBuyOffsetSec,
		DualMarketRatio:         dualMarketRatio,
		HoldToSettleRatio:       holdToSettleRatio,
		SettledMarketCount:      settledMarketCount,
		MarketWinCount:          marketWinCount,
		MarketLossCount:         marketLossCount,
		MarketWinRate:           marketWinRate,
		MarketWinRateWilson:     marketWinRateWilson,
		NoReduceRatio:           noReduceRatio,
		ReduceBeforeSettleRatio: reduceBeforeSettleRatio,
		AddMarketRatio:          addMarketRatio,
		AvgAddsPerMarket:        avgAddsPerMarket,
		PriceBandMarketCount:    priceBandMarketCount,
		PriceBandWinCount:       priceBandWinCount,
		PriceBandLossCount:      priceBandLossCount,
		PriceBandWinRate:        priceBandWinRate,
		PriceBandWinRateWilson:  priceBandWinRateWilson,
		PriceBandROIPct:         priceBandROIPct,
		PriceBandNetPnLUSD:      priceBandNetPnLUSD,
		PriceBandVolumeUSD:      priceBandVolumeUSD,
		CopyableBucketCount:     copyableBucketCount,
		CopyableMarketCount:     copyableMarketCount,
		CopyableWinRate:         copyableWinRate,
		CopyableWinRateWilson:   copyableWinRateWilson,
		CopyableROIPct:          copyableROIPct,
		CopyableNetPnLUSD:       copyableNetPnLUSD,
		BestBucketLabel:         bestBucketLabel,
		BestBucketMarketCount:   bestBucketMarketCount,
		BestBucketWinRateWilson: bestBucketWinRateWilson,
		BestBucketROIPct:        bestBucketROIPct,
		BucketSummary:           bucketSummaryJSON,
		FirstBuyMarketCount:     firstBuyMarketCount,
		FirstBuyWinCount:        firstBuyWinCount,
		FirstBuyLossCount:       firstBuyLossCount,
		FirstBuyWinRate:         firstBuyWinRate,
		FirstBuyWinRateWilson:   firstBuyWinRateWilson,
		FirstBuyROIPct:          firstBuyROIPct,
		FirstBuyNetPnLUSD:       firstBuyNetPnLUSD,
		FirstBuyVolumeUSD:       firstBuyVolumeUSD,
	}, true
}

// parseCloseTs 從 5m crypto eventSlug ("btc-updown-5m-1777927200") 抽 timestamp + 5min
func parseCloseTs(slug string) int64 {
	idx := strings.LastIndex(slug, "-")
	if idx < 0 || idx+1 >= len(slug) {
		return 0
	}
	startTs, err := strconv.ParseInt(slug[idx+1:], 10, 64)
	if err != nil {
		return 0
	}
	return startTs + 300
}

// ── progress helpers ────────────────────────────────────────────────────────

func (s *Scanner) setProgress(p Progress) {
	s.mu.Lock()
	if p.StartedAt == 0 {
		p.StartedAt = s.progress.StartedAt
	}
	s.progress = p
	s.mu.Unlock()
}

func (s *Scanner) setPhase(phase, detail string) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.progress.Detail = detail
	s.mu.Unlock()
}
