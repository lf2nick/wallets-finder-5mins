import { useEffect, useState } from "react";

export type FilterValues = {
  dual_market_ratio_max: number;
  crypto_ratio_min: number;
  hold_to_settle_ratio_min: number;
  min_trades: number;
  settled_markets_min: number;
  market_wilson_min: number;
  no_reduce_ratio_min: number;
  price_band_markets_min: number;
  price_band_roi_min: number;
};

const FIELDS: {
  key: keyof FilterValues;
  label: string;
  tip: string;
  min: number;
  max: number;
  step: number;
}[] = [
  {
    key: "dual_market_ratio_max",
    label: "雙向上限 %",
    tip: "同市場同時買 Up 與 Down 的市場比例；越低越像純方向預測。",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "crypto_ratio_min",
    label: "5m 佔比 %",
    tip: "5 分鐘 crypto 活動佔總活動比例；越高越專注。",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "hold_to_settle_ratio_min",
    label: "Hold %",
    tip: "沒有在結算前整筆退出的市場比例。",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "no_reduce_ratio_min",
    label: "不減倉 %",
    tip: "結算前沒有任何 SELL/減倉的市場比例，比 Hold 更嚴格。",
    min: 0,
    max: 100,
    step: 1,
  },
  {
    key: "min_trades",
    label: "最少交易",
    tip: "BUY 交易筆數下限；保留舊指標，方便和原本結果對照。",
    min: 1,
    max: 10000,
    step: 1,
  },
  {
    key: "settled_markets_min",
    label: "最少市場",
    tip: "已結算 5m 市場數下限；用市場數避免加倉把勝率灌水。",
    min: 1,
    max: 10000,
    step: 1,
  },
  {
    key: "market_wilson_min",
    label: "市場 W95 %",
    tip: "市場級勝率 Wilson 95% 下界；比 raw 勝率更保守。",
    min: 0,
    max: 100,
    step: 0.1,
  },
  {
    key: "price_band_markets_min",
    label: "價格帶市場",
    tip: "平均買價落在 0.3~0.7 的已結算市場數。",
    min: 0,
    max: 10000,
    step: 1,
  },
  {
    key: "price_band_roi_min",
    label: "價格帶 ROI %",
    tip: "只看 0.3~0.7 價格帶市場的 ROI；用來確認我們可跟價格仍是正期望。",
    min: -1000,
    max: 1000,
    step: 0.1,
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
      <div className="card-title">Filter 設定</div>
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
            儲存
          </button>
          <button disabled={busy} onClick={() => onScan()}>
            建立 snapshot
          </button>
        </div>
      </div>
    </div>
  );
}
