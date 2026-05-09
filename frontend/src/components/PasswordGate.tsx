// PasswordGate — 第一次 load 顯示，user 輸入 token 後 store localStorage
import { useState } from "react";
import { setToken } from "../api/client";

export function PasswordGate({ onPass }: { onPass: () => void }) {
  const [pw, setPw] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function tryLogin() {
    setBusy(true);
    setErr("");
    try {
      // 直接打 /api/config（過 auth 的 endpoint）試 token
      const r = await fetch("/api/config", {
        headers: { Authorization: `Bearer ${pw}` },
      });
      if (r.status === 401) {
        setErr("密碼錯誤");
        return;
      }
      if (!r.ok) {
        setErr("HTTP " + r.status);
        return;
      }
      setToken(pw);
      onPass();
    } catch (e: any) {
      setErr(e.message || String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="gate">
      <h2 style={{ margin: 0 }}>🔐 wallets-finder-5mins</h2>
      <p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
        輸入 DASHBOARD_TOKEN
      </p>
      <input
        type="password"
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && tryLogin()}
        autoFocus
      />
      <button
        className="primary"
        style={{ width: "100%" }}
        onClick={tryLogin}
        disabled={busy || !pw}
      >
        {busy ? "驗證中..." : "進入"}
      </button>
      {err && <div style={{ marginTop: 10, color: "var(--red)", fontSize: 12 }}>{err}</div>}
    </div>
  );
}
