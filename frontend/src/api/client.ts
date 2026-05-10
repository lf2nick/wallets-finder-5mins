// API client — Bearer token via localStorage
//
// 流程：
//   1. 第一次 load：localStorage 沒 token → PasswordGate 跳出來輸入
//   2. PasswordGate 試打 /api/health 確認 token 對 → 存進 localStorage
//   3. 後續 fetch 自動加 Authorization: Bearer

const TOKEN_KEY = "wf5m_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...((init?.headers as Record<string, string>) || {}),
  };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const res = await fetch(path, { ...init, headers });
  if (res.status === 401) {
    clearToken();
    throw new ApiError(401, "unauthorized");
  }
  const text = await res.text();
  if (!res.ok) {
    let msg = text;
    try {
      const j = JSON.parse(text);
      msg = j.error || text;
    } catch {
      /* not json */
    }
    throw new ApiError(res.status, msg);
  }
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

export const api = {
  health: () => request<{ ok: boolean }>("/api/health"),

  // candidates + filter
  getConfig: () => request<Record<string, number>>("/api/config"),
  updateConfig: (updates: Record<string, number>) =>
    request<Record<string, number>>("/api/config", {
      method: "PUT",
      body: JSON.stringify({ updates }),
    }),
  getCandidates: (params: Record<string, string | number> = {}) => {
    const q = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => q.set(k, String(v)));
    const qs = q.toString();
    return request<{
      items: Candidate[];
      match_count: number;
      candidate_count: number;
      filter: Filter;
    }>(`/api/candidates${qs ? "?" + qs : ""}`);
  },
  scan: () =>
    request<{ snapshot_id: number; match_count: number; candidate_count: number }>(
      "/api/scan",
      { method: "POST" }
    ),

  // history
  configHistory: (limit = 100) =>
    request<{ items: ConfigChange[]; count: number }>(
      `/api/history/config?limit=${limit}`
    ),
  snapshots: (limit = 50) =>
    request<{ items: Snapshot[]; count: number }>(
      `/api/history/snapshots?limit=${limit}`
    ),
  snapshotDetail: (id: number) =>
    request<{ items: SnapshotMatch[]; count: number }>(
      `/api/history/snapshot?id=${id}`
    ),
  walletHistory: (address: string, limit = 50) =>
    request<{ items: SnapshotMatch[]; count: number }>(
      `/api/history/wallet?address=${address}&limit=${limit}`
    ),

  // observe
  observe: (address: string, alias: string = "") =>
    request<{ ok: boolean; address: string }>("/api/observe", {
      method: "POST",
      body: JSON.stringify({ address, alias }),
    }),
};

// types — 必須跟 backend struct 對齊
export type Candidate = {
  address: string;
  total_trades: number;
  win_count: number;
  lose_count: number;
  win_rate: number;
  roi_pct: number;
  net_pnl_usd: number;
  volume_usd: number;
  crypto_ratio: number;
  dual_market_ratio: number;
  hold_to_settle_ratio: number;
  avg_buy_offset_sec: number;
  profit_loss_ratio: number;
  wash_ratio: number;
  avg_buy_price: number;
  extreme_price_ratio: number;
  settled_market_count: number;
  market_win_count: number;
  market_loss_count: number;
  market_win_rate: number;
  market_win_rate_wilson: number;
  no_reduce_ratio: number;
  reduce_before_settle_ratio: number;
  add_market_ratio: number;
  avg_adds_per_market: number;
  price_band_market_count: number;
  price_band_win_count: number;
  price_band_loss_count: number;
  price_band_win_rate: number;
  price_band_win_rate_wilson: number;
  price_band_roi_pct: number;
  price_band_net_pnl_usd: number;
  price_band_volume_usd: number;
  price_band_market_ratio: number;
  last_trade_at?: string;
  evaluated_at: string;
  already_observed: boolean;
};

export type Filter = {
  DualMaxPct: number;
  CryptoMinPct: number;
  HoldMinPct: number;
  MinTrades: number;
  SettledMarketsMin: number;
  MarketW95MinPct: number;
  NoReduceMinPct: number;
  PriceBandMarketsMin: number;
  PriceBandROIMinPct: number;
  AvgBuyPriceMax: number;
  ExtremePriceMaxPct: number;
  PriceBandRatioMinPct: number;
  SortBy: string;
  Limit: number;
};

export type ConfigChange = {
  id: number;
  key: string;
  old_value?: number;
  new_value: number;
  changed_at: string;
};

export type Snapshot = {
  id: number;
  snapshot_at: string;
  config: Record<string, number>;
  match_count: number;
  candidate_count: number;
};

export type SnapshotMatch = {
  snapshot_id: number;
  address: string;
  win_rate?: number;
  crypto_ratio?: number;
  dual_market_ratio?: number;
  hold_to_settle_ratio?: number;
  total_trades?: number;
  net_pnl_usd?: number;
  market_win_rate_wilson?: number;
  no_reduce_ratio?: number;
  settled_market_count?: number;
  price_band_roi_pct?: number;
  snapshot_at?: string;
};
