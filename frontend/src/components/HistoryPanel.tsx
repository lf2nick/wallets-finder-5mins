// HistoryPanel — 三 tab：設定變動 / Snapshot 清單 / 單一錢包命中歷史
import { useEffect, useState } from "react";
import { api, ConfigChange, Snapshot, SnapshotMatch } from "../api/client";

type Tab = "config" | "snapshots" | "wallet";

type Props = {
  walletAddress?: string;       // 從 CandidatesTable 點 row 帶來的 address
  onClearWallet: () => void;
};

function fmtTime(s: string) {
  const d = new Date(s);
  return `${String(d.getMonth() + 1).padStart(2, "0")}/${String(d.getDate()).padStart(2, "0")} ` +
    `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

export function HistoryPanel({ walletAddress, onClearWallet }: Props) {
  const [tab, setTab] = useState<Tab>("config");

  // 點 row 帶 wallet 進來時自動切 tab
  useEffect(() => {
    if (walletAddress) setTab("wallet");
  }, [walletAddress]);

  return (
    <div className="card side-panel">
      <div className="tabs">
        <div className={`tab ${tab === "config" ? "active" : ""}`} onClick={() => setTab("config")}>
          設定變動
        </div>
        <div className={`tab ${tab === "snapshots" ? "active" : ""}`} onClick={() => setTab("snapshots")}>
          Snapshot
        </div>
        <div className={`tab ${tab === "wallet" ? "active" : ""}`} onClick={() => setTab("wallet")}>
          單一錢包
        </div>
      </div>
      {tab === "config" && <ConfigTab />}
      {tab === "snapshots" && <SnapshotsTab />}
      {tab === "wallet" && (
        <WalletTab address={walletAddress} onClear={onClearWallet} />
      )}
    </div>
  );
}

function ConfigTab() {
  const [items, setItems] = useState<ConfigChange[]>([]);
  const [err, setErr] = useState("");
  useEffect(() => {
    api.configHistory()
      .then((d) => setItems(d.items || []))
      .catch((e) => setErr(String(e)));
  }, []);
  if (err) return <div style={{ color: "var(--red)" }}>{err}</div>;
  if (items.length === 0) return <div className="muted">尚無變動紀錄</div>;
  return (
    <>
      {items.map((c) => (
        <div key={c.id} className="history-row">
          <span className="muted mono">{fmtTime(c.changed_at)}</span>
          <span>{c.key}</span>
          <span className="mono">
            {c.old_value != null ? c.old_value : "—"} → <strong>{c.new_value}</strong>
          </span>
        </div>
      ))}
    </>
  );
}

function SnapshotsTab() {
  const [items, setItems] = useState<Snapshot[]>([]);
  const [err, setErr] = useState("");
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [matches, setMatches] = useState<SnapshotMatch[]>([]);

  useEffect(() => {
    api.snapshots()
      .then((d) => setItems(d.items || []))
      .catch((e) => setErr(String(e)));
  }, []);

  async function expand(id: number) {
    if (expandedId === id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(id);
    setMatches([]);
    try {
      const d = await api.snapshotDetail(id);
      setMatches(d.items || []);
    } catch (e) {
      setErr(String(e));
    }
  }

  if (err) return <div style={{ color: "var(--red)" }}>{err}</div>;
  if (items.length === 0) return <div className="muted">尚無 snapshot — 點「立即掃描」建立</div>;

  return (
    <>
      {items.map((s) => (
        <div key={s.id}>
          <div className="history-row" style={{ cursor: "pointer" }} onClick={() => expand(s.id)}>
            <span className="muted mono">{fmtTime(s.snapshot_at)}</span>
            <span className="mono">{s.match_count} 命中</span>
            <span className="muted" style={{ fontSize: 11 }}>
              {Object.entries(s.config).map(([k, v]) => `${k}=${v}`).join(" / ")}
            </span>
          </div>
          {expandedId === s.id && (
            <div style={{ paddingLeft: 16, fontSize: 11 }}>
              {matches.length === 0 && <div className="muted">載入中...</div>}
              {matches.slice(0, 30).map((m) => (
                <div key={m.address} className="mono" style={{ padding: "2px 0" }}>
                  {m.address.slice(0, 6)}…{m.address.slice(-4)}
                  {m.net_pnl_usd != null && (
                    <span style={{ marginLeft: 8 }}>${m.net_pnl_usd.toFixed(0)}</span>
                  )}
                </div>
              ))}
              {matches.length > 30 && <div className="muted">... +{matches.length - 30} more</div>}
            </div>
          )}
        </div>
      ))}
    </>
  );
}

function WalletTab({ address, onClear }: { address?: string; onClear: () => void }) {
  const [items, setItems] = useState<SnapshotMatch[]>([]);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!address) {
      setItems([]);
      return;
    }
    setErr("");
    api.walletHistory(address)
      .then((d) => setItems(d.items || []))
      .catch((e) => setErr(String(e)));
  }, [address]);

  if (!address) return <div className="muted">點 candidates 表的 address 查詢該錢包命中歷史</div>;

  return (
    <>
      <div style={{ marginBottom: 8, fontSize: 11 }}>
        <span className="mono">{address}</span>
        <button className="btn-mini" style={{ marginLeft: 8 }} onClick={onClear}>清除</button>
      </div>
      {err && <div style={{ color: "var(--red)" }}>{err}</div>}
      {items.length === 0 ? (
        <div className="muted" style={{ fontSize: 12 }}>該錢包未在任何 snapshot 命中過</div>
      ) : (
        items.map((m) => (
          <div key={m.snapshot_id} className="history-row">
            <span className="muted mono">{m.snapshot_at && fmtTime(m.snapshot_at)}</span>
            <span className="mono">${m.net_pnl_usd?.toFixed(0) ?? "—"}</span>
            <span className="muted" style={{ fontSize: 11 }}>
              w={m.win_rate?.toFixed(0)}% / 5m={m.crypto_ratio?.toFixed(0)}% /
              dual={m.dual_market_ratio?.toFixed(0)}% / hold={m.hold_to_settle_ratio?.toFixed(0)}%
            </span>
          </div>
        ))
      )}
    </>
  );
}
