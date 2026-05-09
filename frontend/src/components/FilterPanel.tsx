// FilterPanel — 3 個 ratio + min_trades 設定欄位 + 儲存 / 立即 scan
import { useEffect, useState } from "react";

export type FilterValues = {
  dual_market_ratio_max: number;
  crypto_ratio_min: number;
  hold_to_settle_ratio_min: number;
  min_trades: number;
};

const FIELDS: { key: keyof FilterValues; label: string; tip: string; min: number; max: number; step: number }[] = [
  {
    key: "dual_market_ratio_max",
    label: "雙向持倉 ≤ (%)",
    tip: "同一個 market 同時押 Up 和 Down 的比例上限。低 = 不對沖",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "crypto_ratio_min",
    label: "5min 比例 ≥ (%)",
    tip: "5min crypto 活動 / 全部活動 × 100。高 = 專攻 5min 加密",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "hold_to_settle_ratio_min",
    label: "Hold 結算 ≥ (%)",
    tip: "hold 到結算的市場 / 已結算市場 × 100。高 = directional trader",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "min_trades",
    label: "最少筆數",
    tip: "total_trades ≥ 此值。樣本太小不可信",
    min: 1,
    max: 10000,
    step: 1,
  },
];

type Props = {
  initial: FilterValues;
  onSave: (v: FilterValues) => Promise<void>;
  onScan: () => Promise<void>;
  busy?: boolean;
};

export function FilterPanel({ initial, onSave, onScan, busy }: Props) {
  const [v, setV] = useState<FilterValues>(initial);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setV(initial);
    setDirty(false);
  }, [initial]);

  function update(key: keyof FilterValues, val: number) {
    setV((prev) => ({ ...prev, [key]: val }));
    setDirty(true);
  }

  return (
    <div className="card">
      <div className="card-title">📊 Filter 設定</div>
      <div className="filter-row">
        {FIELDS.map((f) => (
          <label key={f.key} title={f.tip}>
            <span>{f.label}</span>
            <input
              type="number"
              value={v[f.key]}
              min={f.min}
              max={f.max}
              step={f.step}
              onChange={(e) => update(f.key, parseFloat(e.target.value) || 0)}
            />
          </label>
        ))}
        <div className="actions">
          <button
            className="primary"
            disabled={!dirty || busy}
            onClick={async () => {
              await onSave(v);
              setDirty(false);
            }}
          >
            💾 儲存
          </button>
          <button disabled={busy} onClick={() => onScan()}>
            🔍 立即掃描
          </button>
        </div>
      </div>
    </div>
  );
}
