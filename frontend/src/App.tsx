// App — 主畫面組合
import { useCallback, useEffect, useState } from "react";
import { api, ApiError, Candidate, getToken, clearToken } from "./api/client";
import { PasswordGate } from "./components/PasswordGate";
import { FilterPanel, FilterValues } from "./components/FilterPanel";
import { CandidatesTable } from "./components/CandidatesTable";
import { HistoryPanel } from "./components/HistoryPanel";
import { ScanPanel } from "./components/ScanPanel";

type SortKey =
  | "net_pnl_usd"
  | "win_rate"
  | "roi_pct"
  | "total_trades"
  | "hold_to_settle_ratio"
  | "dual_market_ratio"
  | "crypto_ratio";

type Toast = { msg: string; kind: "success" | "error"; id: number } | null;

export default function App() {
  const [authed, setAuthed] = useState<boolean>(!!getToken());
  const [filter, setFilter] = useState<FilterValues | null>(null);
  const [items, setItems] = useState<Candidate[]>([]);
  const [matchCount, setMatchCount] = useState(0);
  const [poolSize, setPoolSize] = useState(0);
  const [sortBy, setSortBy] = useState<SortKey>("net_pnl_usd");
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [walletForHistory, setWalletForHistory] = useState<string | undefined>();
  const [lastRefreshed, setLastRefreshed] = useState<string>("");

  const showToast = useCallback((msg: string, kind: "success" | "error" = "success") => {
    const id = Date.now();
    setToast({ msg, kind, id });
    setTimeout(() => setToast((t) => (t?.id === id ? null : t)), 4000);
  }, []);

  // 載入當前 config + 第一輪 candidates
  const refresh = useCallback(async () => {
    setBusy(true);
    try {
      const cfg = await api.getConfig();
      const fv: FilterValues = {
        dual_market_ratio_max: cfg.dual_market_ratio_max ?? 25,
        crypto_ratio_min: cfg.crypto_ratio_min ?? 80,
        hold_to_settle_ratio_min: cfg.hold_to_settle_ratio_min ?? 80,
        min_trades: cfg.min_trades ?? 30,
      };
      setFilter(fv);
      const d = await api.getCandidates({ sort: sortBy });
      setItems(d.items || []);
      setMatchCount(d.match_count);
      setPoolSize(d.candidate_count);
      const now = new Date();
      setLastRefreshed(`${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}`);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        setAuthed(false);
        return;
      }
      showToast(String(e), "error");
    } finally {
      setBusy(false);
    }
  }, [sortBy, showToast]);

  useEffect(() => {
    if (!authed) return;
    refresh();
  }, [authed, refresh]);

  async function saveConfig(v: FilterValues) {
    try {
      await api.updateConfig(v);
      showToast("✅ filter 已儲存", "success");
      await refresh();
    } catch (e) {
      showToast(`儲存失敗: ${e}`, "error");
    }
  }

  async function runScan() {
    setBusy(true);
    try {
      const r = await api.scan();
      showToast(`✅ scan 完成 — snapshot #${r.snapshot_id}, 命中 ${r.match_count}`, "success");
      await refresh();
    } catch (e) {
      showToast(`scan 失敗: ${e}`, "error");
    } finally {
      setBusy(false);
    }
  }

  function logout() {
    clearToken();
    setAuthed(false);
  }

  if (!authed) {
    return <PasswordGate onPass={() => setAuthed(true)} />;
  }
  if (!filter) {
    return <div className="app"><div className="muted">載入中...</div></div>;
  }

  return (
    <div className="app">
      <h1>
        🔎 wallets-finder-5mins
        <span className="muted" style={{ fontSize: 12, marginLeft: 12 }}>
          {matchCount} 命中 / {poolSize} candidates ・ 上次刷新 {lastRefreshed}
        </span>
        <span style={{ float: "right", fontSize: 12 }}>
          <button className="btn-mini" onClick={() => refresh()}>🔄 重整</button>
          <button className="btn-mini" style={{ marginLeft: 6 }} onClick={logout}>登出</button>
        </span>
      </h1>

      <FilterPanel initial={filter} onSave={saveConfig} onScan={runScan} busy={busy} />
      <ScanPanel
        token={getToken() || ""}
        onScanComplete={() => refresh()}
        toast={showToast}
      />

      <div style={{ display: "grid", gridTemplateColumns: "1fr 360px", gap: 14 }}>
        <CandidatesTable
          items={items}
          sortBy={sortBy}
          onSortChange={(k) => setSortBy(k)}
          onObserved={() => refresh()}
          onWalletClick={(a) => setWalletForHistory(a)}
          toast={showToast}
        />
        <HistoryPanel
          walletAddress={walletForHistory}
          onClearWallet={() => setWalletForHistory(undefined)}
        />
      </div>

      {toast && <div className={`toast ${toast.kind}`}>{toast.msg}</div>}
    </div>
  );
}
