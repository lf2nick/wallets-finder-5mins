# wallets-finder-5mins

Polymarket 5min crypto market 錢包篩選器。從 `poly-tracker` 已掃描的
`crypto_wallet_candidates` 池中，根據 3 個 filter 找出「值得跟單」的錢包：

1. **dual_market_ratio ≤ 25%** — 幾乎不雙向持倉
2. **crypto_ratio ≥ 80%** — 專攻 5min 加密漲跌
3. **hold_to_settle_ratio ≥ 80%** — hold 到結算（不結算前出清）

跟 `poly-tracker` 共用同一個 `poly_tracker` PostgreSQL database：
- 唯讀 `crypto_wallet_candidates`（poly-tracker 的 cryptofinder 寫入）
- 自己擁有 `wf5m_*` 前綴的 3 張表（filter 設定 / 變動 audit / 命中 snapshot）
- 一鍵「加入觀察」會 INSERT 進 `copy_sim_wallets`（poly-tracker 的觀察錢包）

## 架構

```
[browser :3002] → nginx → /         → React SPA static
                        → /api/*    → Go backend (內部 :8080)
                                          ↓
                                    poly_tracker DB
```

## Stack

- **Backend**: Go 1.22+, `database/sql` + `lib/pq`, no framework
- **Frontend**: Vite + React 18 + TypeScript, plain CSS
- **Infra**: Docker Compose, nginx (proxy + static)

## 本機開發

```bash
# 1. 設定 .env
cp .env.example .env
# 填 DB_PASSWORD + DASHBOARD_TOKEN

# 2. Backend (Go)
cd backend
go run ./cmd/server
# → :8080

# 3. Frontend (Vite dev)
cd frontend
npm install
npm run dev
# → :5173 (proxy /api → :8080)
```

## Docker 部署

```bash
docker compose up -d --build
# → frontend on http://localhost:3002
```

## EC2 部署

```bash
./deploy.sh
# → http://<EC2_HOST>:3002
```

詳見 `deploy.sh` — 流程跟 poly-tracker 一樣（git push → ssh → git pull → docker rebuild）。
