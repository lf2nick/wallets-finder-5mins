// CandidatesTable — 命中清單表格 + 觀察按鈕
import { useState } from "react";
import { Candidate, api, ApiError } from "../api/client";

type SortKey =
  | "net_pnl_usd"
  | "win_rate"
  | "roi_pct"
  | "total_trades"
  | "hold_to_settle_ratio"
  | "dual_market_ratio"
  | "crypto_ratio";

type Props = {
  items: Candidate[];
  sortBy: SortKey;
  onSortChange: (k: SortKey) => void;
  onObserved: (address: string) => void;
  onWalletClick?: (address: string) => void;
  toast: (msg: string, kind?: "success" | "error") => void;
};

const COLS: { key: SortKey; label: string; tip?: string; fmt?: (c: Candidate) => string }[] = [
  { key: "total_trades", label: "筆數", fmt: (c) => String(c.total_trades) },
  { key: "win_rate", label: "勝率%", fmt: (c) => c.win_rate.toFixed(1) },
  { key: "net_pnl_usd", label: "NetPnL ($)", fmt: (c) => fmtUSD(c.net_pnl_usd) },
  { key: "roi_pct", label: "ROI%", fmt: (c) => c.roi_pct.toFixed(1) },
  { key: "crypto_ratio", label: "5m 比例%", fmt: (c) => c.crypto_ratio.toFixed(0) },
  { key: "dual_market_ratio", label: "雙向%", fmt: (c) => c.dual_market_ratio.toFixed(0) },
  { key: "hold_to_settle_ratio", label: "Hold%", fmt: (c) => c.hold_to_settle_ratio.toFixed(0) },
];

function fmtUSD(n: number) {
  const sign = n >= 0 ? "+" : "";
  return `${sign}$${n.toFixed(0)}`;
}

function shortAddr(a: string) {
  return `${a.slice(0, 6)}…${a.slice(-4)}`;
}

export function CandidatesTable({ items, sortBy, onSortChange, onObserved, onWalletClick, toast }: Props) {
  const [busyAddr, setBusyAddr] = useState<string | null>(null);

  async function observe(addr: string) {
    setBusyAddr(addr);
    try {
      await api.observe(addr);
      toast(`✅ 已加入觀察 ${shortAddr(addr)}`, "success");
      onObserved(addr);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      toast(`觀察失敗: ${msg}`, "error");
    } finally {
      setBusyAddr(null);
    }
  }

  if (items.length === 0) {
    return <div className="card"><span className="muted">沒有符合 filter 的 wallet</span></div>;
  }

  return (
    <div className="card" style={{ padding: 0 }}>
      <div style={{ overflowX: "auto", maxHeight: "60vh" }}>
        <table className="candidates">
          <thead>
            <tr>
              <th style={{ minWidth: 140 }}>address</th>
              {COLS.map((c) => (
                <th
                  key={c.key}
                  onClick={() => onSortChange(c.key)}
                  style={{ minWidth: 70 }}
                  title={c.tip}
                >
                  {c.label}
                  {sortBy === c.key && <span style={{ marginLeft: 4 }}>▼</span>}
                </th>
              ))}
              <th style={{ minWidth: 80 }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {items.map((c) => (
              <tr key={c.address}>
                <td>
                  <a
                    className="address"
                    href={`https://polymarket.com/profile/${c.address}`}
                    target="_blank"
                    rel="noopener"
                    title={c.address}
                    onClick={(e) => {
                      if (onWalletClick) {
                        e.preventDefault();
                        onWalletClick(c.address);
                      }
                    }}
                  >
                    {shortAddr(c.address)} 🔗
                  </a>
                </td>
                {COLS.map((col) => (
                  <td key={col.key} className="mono">
                    {col.fmt!(c)}
                  </td>
                ))}
                <td>
                  {c.already_observed ? (
                    <span className="btn-mini observed">✓ 觀察中</span>
                  ) : (
                    <button
                      className="btn-mini"
                      disabled={busyAddr === c.address}
                      onClick={() => observe(c.address)}
                    >
                      {busyAddr === c.address ? "..." : "+ 觀察"}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
