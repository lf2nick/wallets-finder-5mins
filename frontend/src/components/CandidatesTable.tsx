import { useState } from "react";
import { Candidate, api, ApiError } from "../api/client";

type SortKey =
  | "net_pnl_usd"
  | "win_rate"
  | "roi_pct"
  | "total_trades"
  | "hold_to_settle_ratio"
  | "dual_market_ratio"
  | "crypto_ratio"
  | "settled_market_count"
  | "market_win_rate_wilson"
  | "no_reduce_ratio"
  | "add_market_ratio"
  | "price_band_market_count"
  | "price_band_win_rate_wilson"
  | "price_band_roi_pct";

type Props = {
  items: Candidate[];
  sortBy: SortKey;
  onSortChange: (k: SortKey) => void;
  onObserved: (address: string) => void;
  onWalletClick?: (address: string) => void;
  toast: (msg: string, kind?: "success" | "error") => void;
};

const COLS: { key: SortKey; label: string; tip?: string; fmt: (c: Candidate) => string }[] = [
  { key: "settled_market_count", label: "市場", tip: "已結算 5m 市場數。", fmt: (c) => String(c.settled_market_count) },
  { key: "market_win_rate_wilson", label: "市場W95", tip: "市場級勝率 Wilson 95% 下界。", fmt: (c) => fmtPct(c.market_win_rate_wilson, 1) },
  { key: "win_rate", label: "交易勝率", tip: "舊口徑：加倉會放大交易勝負次數。", fmt: (c) => fmtPct(c.win_rate, 1) },
  { key: "price_band_market_count", label: "0.3~0.7數", tip: "平均買價落在 0.3~0.7 的已結算市場數。", fmt: (c) => String(c.price_band_market_count) },
  { key: "price_band_win_rate_wilson", label: "價格帶W95", tip: "0.3~0.7 價格帶的市場級 W95。", fmt: (c) => fmtPct(c.price_band_win_rate_wilson, 1) },
  { key: "price_band_roi_pct", label: "價格帶ROI", tip: "只看 0.3~0.7 價格帶的 ROI。", fmt: (c) => fmtPct(c.price_band_roi_pct, 1) },
  { key: "no_reduce_ratio", label: "不減倉", tip: "結算前沒有 SELL/減倉的市場比例。", fmt: (c) => fmtPct(c.no_reduce_ratio, 0) },
  { key: "add_market_ratio", label: "加倉", tip: "同市場同方向買超過一次的比例。", fmt: (c) => fmtPct(c.add_market_ratio, 0) },
  { key: "dual_market_ratio", label: "雙向", tip: "同市場同時買 Up 與 Down 的比例。", fmt: (c) => fmtPct(c.dual_market_ratio, 0) },
  { key: "crypto_ratio", label: "5m佔比", fmt: (c) => fmtPct(c.crypto_ratio, 0) },
  { key: "net_pnl_usd", label: "NetPnL", fmt: (c) => fmtUSD(c.net_pnl_usd) },
  { key: "roi_pct", label: "ROI", fmt: (c) => fmtPct(c.roi_pct, 1) },
  { key: "total_trades", label: "交易", fmt: (c) => String(c.total_trades) },
  { key: "hold_to_settle_ratio", label: "Hold", fmt: (c) => fmtPct(c.hold_to_settle_ratio, 0) },
];

function fmtUSD(n: number) {
  const sign = n >= 0 ? "+" : "";
  return `${sign}$${n.toFixed(0)}`;
}

function fmtPct(n: number, digits = 1) {
  return `${n.toFixed(digits)}%`;
}

function shortAddr(a: string) {
  return `${a.slice(0, 6)}...${a.slice(-4)}`;
}

export function CandidatesTable({ items, sortBy, onSortChange, onObserved, onWalletClick, toast }: Props) {
  const [busyAddr, setBusyAddr] = useState<string | null>(null);

  async function observe(addr: string) {
    setBusyAddr(addr);
    try {
      await api.observe(addr);
      toast(`已加入觀察 ${shortAddr(addr)}`, "success");
      onObserved(addr);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      toast(`加入觀察失敗：${msg}`, "error");
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
                  style={{ minWidth: 76 }}
                  title={c.tip}
                >
                  {c.label}
                  {sortBy === c.key && <span style={{ marginLeft: 4 }}>↓</span>}
                </th>
              ))}
              <th style={{ minWidth: 84 }}>觀察</th>
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
                    {shortAddr(c.address)}
                  </a>
                </td>
                {COLS.map((col) => (
                  <td key={col.key} className="mono">
                    {col.fmt(c)}
                  </td>
                ))}
                <td>
                  {c.already_observed ? (
                    <span className="btn-mini observed">已觀察</span>
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
