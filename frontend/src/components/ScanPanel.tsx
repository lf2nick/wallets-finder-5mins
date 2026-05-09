// ScanPanel — 觸發 cryptofinder scan + 顯示進度
//
// scan 走背景跑 ~3-8 min，所以前端打 /api/cryptofinder/scan 後 poll
// /api/cryptofinder/status 拿 progress 直到 phase=done|error。
import { useEffect, useRef, useState } from "react";

const API = {
  trigger: async (token: string, days: number, minTrades: number) => {
    const r = await fetch(
      `/api/cryptofinder/scan?days=${days}&min_trades=${minTrades}`,
      {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      }
    );
    if (!r.ok) {
      const t = await r.text();
      throw new Error(`HTTP ${r.status}: ${t}`);
    }
    return r.json();
  },
  status: async (token: string) => {
    const r = await fetch("/api/cryptofinder/status", {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    return r.json();
  },
};

type Progress = {
  phase: string;
  detail?: string;
  markets_probed?: number;
  markets_total?: number;
  wallets_evaluated?: number;
  wallets_total?: number;
  started_at?: number;
};

type LastRun = {
  id: number;
  started_at: string;
  finished_at?: string;
  days_window: number;
  min_trades: number;
  seed_count: number;
  candidate_count: number;
  error?: string;
};

type Props = {
  token: string;
  onScanComplete: () => void; // 給 App 用，scan 完幫忙刷 candidates
  toast: (msg: string, kind?: "success" | "error") => void;
};

export function ScanPanel({ token, onScanComplete, toast }: Props) {
  const [days, setDays] = useState(14);
  const [minTrades, setMinTrades] = useState(30);
  const [running, setRunning] = useState(false);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [lastRun, setLastRun] = useState<LastRun | null>(null);
  const pollTimer = useRef<number | null>(null);

  // 啟動時拉一次 status — 知道有沒有上次 scan 紀錄 + 是不是有 scan 在跑
  useEffect(() => {
    refreshStatus();
    return () => {
      if (pollTimer.current) window.clearInterval(pollTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function startPolling() {
    if (pollTimer.current) window.clearInterval(pollTimer.current);
    pollTimer.current = window.setInterval(refreshStatus, 3000); // 每 3s 拉一次
  }

  function stopPolling() {
    if (pollTimer.current) {
      window.clearInterval(pollTimer.current);
      pollTimer.current = null;
    }
  }

  async function refreshStatus() {
    try {
      const s = await API.status(token);
      const wasRunning = running;
      setRunning(!!s.running);
      setProgress(s.progress || null);
      setLastRun(s.last_run || null);
      // running false → done：通知 caller 重抓 candidates
      if (wasRunning && !s.running) {
        stopPolling();
        if (s.progress?.phase === "done") {
          toast(`✅ scan 完成（${s.progress?.detail || ""}）`, "success");
          onScanComplete();
        } else if (s.progress?.phase === "error") {
          toast(`scan 失敗：${s.progress?.detail || "unknown"}`, "error");
        }
      } else if (!wasRunning && s.running) {
        // 外部觸發的 scan：開始 poll
        startPolling();
      }
    } catch (e) {
      // 靜默 — 可能 token 失效，主畫面會重定向到 PasswordGate
    }
  }

  async function triggerScan() {
    if (!confirm(
      `確定要 trigger 一次 cryptofinder scan?\n\n` +
      `視窗: ${days} 天\n最少筆數: ${minTrades}\n\n` +
      `預計 3-8 分鐘完成（4 個資產 × 並行 8 worker）。\n` +
      `背景跑，這頁會 poll 進度。`
    )) return;
    try {
      await API.trigger(token, days, minTrades);
      toast("scan 已觸發 — 等 3-8 分鐘", "success");
      setRunning(true);
      setProgress({ phase: "seeding" });
      startPolling();
    } catch (e: any) {
      toast(`觸發失敗: ${e.message || e}`, "error");
    }
  }

  return (
    <div className="card">
      <div className="card-title">🔍 Cryptofinder Scan</div>
      <div style={{ display: "flex", gap: 14, alignItems: "end", flexWrap: "wrap" }}>
        <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12, color: "var(--muted)" }}>
          <span>窗口（天）</span>
          <input
            type="number"
            value={days}
            min={1}
            max={90}
            onChange={(e) => setDays(parseInt(e.target.value, 10) || 14)}
            disabled={running}
            style={{ width: 80 }}
          />
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12, color: "var(--muted)" }}>
          <span>最少筆數</span>
          <input
            type="number"
            value={minTrades}
            min={1}
            max={10000}
            onChange={(e) => setMinTrades(parseInt(e.target.value, 10) || 30)}
            disabled={running}
            style={{ width: 80 }}
          />
        </label>
        <button className="primary" onClick={triggerScan} disabled={running}>
          {running ? "⏳ 掃描中..." : "🔍 立即掃描"}
        </button>
        <button onClick={refreshStatus}>🔄 刷新狀態</button>
      </div>

      {running && progress && (
        <div style={{ marginTop: 12, padding: "8px 12px", background: "var(--card-hi)", borderRadius: 4, fontSize: 12 }}>
          <div>
            <strong>Phase:</strong> <span className="accent">{progress.phase}</span>
            {progress.started_at && (
              <span className="muted" style={{ marginLeft: 8 }}>
                ({Math.floor((Date.now() - progress.started_at) / 1000)}s)
              </span>
            )}
          </div>
          {progress.detail && <div className="muted" style={{ marginTop: 4 }}>{progress.detail}</div>}
          {progress.phase === "seeding" && progress.markets_total ? (
            <ProgressBar value={progress.markets_probed || 0} max={progress.markets_total} />
          ) : null}
          {progress.phase === "evaluating" && progress.wallets_total ? (
            <ProgressBar value={progress.wallets_evaluated || 0} max={progress.wallets_total} />
          ) : null}
        </div>
      )}

      {!running && lastRun && (
        <div style={{ marginTop: 12, fontSize: 11 }} className="muted">
          上次 scan：
          {lastRun.finished_at ? (
            <>
              {fmtTime(lastRun.finished_at)} —
              <span className="mono"> seed={lastRun.seed_count} cand={lastRun.candidate_count}</span>
              {lastRun.error && <span className="red"> error: {lastRun.error}</span>}
            </>
          ) : (
            <>{fmtTime(lastRun.started_at)} (未完成)</>
          )}
        </div>
      )}
    </div>
  );
}

function ProgressBar({ value, max }: { value: number; max: number }) {
  const pct = max > 0 ? (value / max) * 100 : 0;
  return (
    <div style={{ marginTop: 6 }}>
      <div style={{ background: "var(--border)", borderRadius: 3, height: 6, overflow: "hidden" }}>
        <div style={{ background: "var(--accent)", width: `${pct}%`, height: "100%" }} />
      </div>
      <div className="muted mono" style={{ fontSize: 10, marginTop: 2 }}>
        {value} / {max} ({pct.toFixed(1)}%)
      </div>
    </div>
  );
}

function fmtTime(s: string) {
  const d = new Date(s);
  return `${String(d.getMonth() + 1).padStart(2, "0")}/${String(d.getDate()).padStart(2, "0")} ` +
    `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
