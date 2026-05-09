#!/usr/bin/env bash
# wallets-finder-5mins — 一鍵部署到 EC2
#
# 流程跟 poly-tracker/deploy.sh 一樣：
#   git push → ssh → cd ~/wallets-finder-5mins → git pull → docker compose up --build
#
# 前置條件（一次性）：
#   - EC2 已 git clone https://github.com/lf2nick/wallets-finder-5mins.git ~/wallets-finder-5mins
#   - ~/wallets-finder-5mins/.env 已設定（DB_PASSWORD, DASHBOARD_TOKEN）
#   - poly_tracker DB 已存在於 crypto-db 容器

set -euo pipefail

EC2_HOST="${EC2_HOST:-108.131.217.27}"
EC2_USER="${EC2_USER:-ubuntu}"
EC2_KEY="${EC2_KEY:-C:/Users/lf2ni/source/crypto-predictor-key.pem}"
EC2_DIR="${EC2_DIR:-/home/ubuntu/wallets-finder-5mins}"

echo "🚀 Deploying to ${EC2_USER}@${EC2_HOST}:${EC2_DIR}"
echo ""

echo "📤 本地 git push..."
git push origin main || { echo "❌ git push 失敗，中止 deploy"; exit 1; }
echo ""

ssh -i "$EC2_KEY" -o StrictHostKeyChecking=accept-new "${EC2_USER}@${EC2_HOST}" "bash -se" <<EOF
set -euo pipefail
cd "$EC2_DIR"

echo "📥 git pull..."
git pull --ff-only origin main

echo ""
echo "🔨 docker compose build + up..."
docker compose -f docker-compose.yml -f docker-compose.ec2.yml up -d --build wf5m-backend wf5m-frontend

echo ""
echo "🧹 清 24h 前的 docker build cache..."
docker builder prune --filter until=24h -f || true

echo ""
echo "⏳ 等 backend 就緒..."
for i in {1..30}; do
  if docker logs wf5m-backend 2>&1 | tail -50 | grep -q "HTTP server listening"; then
    echo "✅ backend 起來了"
    break
  fi
  sleep 1
done

echo ""
echo "📋 啟動 log（最後 15 行）"
echo "──────────────────────────────────────"
docker logs wf5m-backend --tail 15 2>&1
echo "──────────────────────────────────────"

echo ""
echo "📦 容器狀態"
docker ps --format "table {{.Names}}\t{{.Status}}" | grep -E "wf5m|NAMES"
EOF

echo ""
echo "🎉 Deploy 完成！dashboard: http://${EC2_HOST}:3002"
