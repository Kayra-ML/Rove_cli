import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

export type UsageSnapshot = {
  totalTokens: number;
  promptTokens: number;
  completionTokens: number;
  calls: number;
  activeAgents: number;
  days: number;
  uptime?: string;
};

export function compact(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0";
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) {
    const k = n / 1000;
    const s = (k >= 100 ? k.toFixed(0) : k.toFixed(1)).replace(/\.0$/, "");
    return `${s}k`;
  }
  return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
}

export function UsageCard() {
  const { lang } = usePrefs();
  const [u, setU] = useState<UsageSnapshot | null>(null);

  const load = useCallback(async () => {
    try {
      const raw = await rpc<UsageSnapshot>("usage.get");
      if (raw && typeof raw === "object") setU(raw);
    } catch {
      /* daemon down */
    }
  }, []);

  useEffect(() => {
    void load();
    const id = window.setInterval(() => void load(), 8000);
    return () => window.clearInterval(id);
  }, [load]);

  const tokens = u?.totalTokens ?? 0;
  const agents = u?.activeAgents ?? 0;
  const days = u?.days ?? 0;

  return (
    <div className="usage-card" title={u?.uptime ? `uptime ${u.uptime}` : undefined}>
      <div className="section-label">{t("usage", lang)}</div>
      <div className="usage-grid">
        <div className="usage-cell">
          <div className="usage-val">{compact(tokens)}</div>
          <div className="usage-key">{t("tokens", lang)}</div>
        </div>
        <div className="usage-cell">
          <div className="usage-val">{agents}</div>
          <div className="usage-key">{t("activeAgents", lang)}</div>
        </div>
        <div className="usage-cell">
          <div className="usage-val">{days}</div>
          <div className="usage-key">{t("daysLive", lang)}</div>
        </div>
      </div>
    </div>
  );
}
