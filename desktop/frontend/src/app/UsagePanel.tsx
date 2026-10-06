import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import { usePrefs } from "~/hooks/usePrefs";
import { t, type Lang } from "~/lib/i18n";
import { compact } from "./UsageCard";

// The usage report the daemon builds from its ledger (internal/usage).
export type Tally = {
  calls: number;
  sentTokens: number;
  recvTokens: number;
  sentChars: number;
  recvChars: number;
  reportedCalls: number;
  reportedIn: number;
  reportedOut: number;
  cached: number;
  oursOnReported: number;
};
export type UsageReport = {
  days: (Tally & { date: string })[];
  models: (Tally & { provider: string; model: string })[];
  total: Tally;
  since: string;
  until: string;
};

const RANGES = [7, 30, 90] as const;

// UsagePanel is Settings → Usage: tokens per day and per model. The figure
// it leads with is Rove's own count — every character it actually sent to a
// model and got back, turned into tokens — not what a provider says it
// charged. The providers' numbers stand beside it, over the same calls, with
// the gap between the two spelled out.
export function UsagePanel() {
  const { lang } = usePrefs();
  const [days, setDays] = useState<number>(30);
  const [r, setR] = useState<UsageReport | null>(null);
  const [table, setTable] = useState(false);

  const load = useCallback(async () => {
    try {
      const got = await rpc<UsageReport>("usage.report", { days });
      // an empty list may come as null
      if (got) setR({ ...got, days: got.days ?? [], models: got.models ?? [] });
    } catch { /* keep */ }
  }, [days]);
  useEffect(() => {
    void load();
    // a new call lands in the ledger: refresh, at most every few seconds
    let soon: ReturnType<typeof setTimeout> | null = null;
    const off = subscribeEvents("usage.recorded", () => {
      if (!soon) soon = setTimeout(() => { soon = null; void load(); }, 3000);
    });
    return () => { off(); if (soon) clearTimeout(soon); };
  }, [load]);

  const total = r ? r.total.sentTokens + r.total.recvTokens : 0;
  const active = r ? r.days.filter((d) => d.calls > 0).length : 0;

  return (
    <div className="usage">
      <div className="usage-head">
        <div>
          <h2>{t("usageTitle", lang)}</h2>
          <p className="split-lead">{t("usageLead", lang)}</p>
        </div>
        <div className="seg usage-range" role="tablist" aria-label={t("usageRange", lang)}>
          {RANGES.map((n) => (
            <button key={n} type="button" role="tab" aria-selected={days === n} className={days === n ? "on" : ""} onClick={() => setDays(n)}>
              {t("usageDays", lang).replace("{n}", String(n))}
            </button>
          ))}
        </div>
      </div>

      {!r ? <div className="usage-loading" /> : (
        <>
          <div className="usage-tiles">
            <Tile label={t("usageTotal", lang)} value={compact(total)} sub={t("usageOurCount", lang)} strong />
            <Tile label={t("usagePerDay", lang)} value={compact(active ? total / active : 0)} sub={t("usageActiveDays", lang).replace("{n}", String(active))} />
            <Tile label={t("usageSent", lang)} value={compact(r.total.sentTokens)} sub={`${compact(r.total.sentChars)} ${t("usageChars", lang)}`} dot="sent" />
            <Tile label={t("usageRecv", lang)} value={compact(r.total.recvTokens)} sub={`${compact(r.total.recvChars)} ${t("usageChars", lang)}`} dot="recv" />
            <Tile label={t("usageCalls", lang)} value={compact(r.total.calls)} sub={`${r.since} – ${r.until}`} />
          </div>

          <Compare t={r.total} lang={lang} />

          <section className="usage-chart-wrap">
            <div className="usage-chart-head">
              <strong>{t("usageByDay", lang)}</strong>
              <div className="usage-legend" aria-hidden>
                <span><i className="sent" />{t("usageSent", lang)}</span>
                <span><i className="recv" />{t("usageRecv", lang)}</span>
              </div>
              <span className="tw-spacer" />
              <button type="button" className="ghost usage-table-btn" aria-pressed={table} onClick={() => setTable((v) => !v)}>
                {t(table ? "usageAsChart" : "usageAsTable", lang)}
              </button>
            </div>
            {total === 0 ? <p className="map-muted usage-empty">{t("usageEmpty", lang)}</p>
              : table ? <DayTable r={r} lang={lang} /> : <DayChart r={r} lang={lang} />}
          </section>

          <section className="usage-models">
            <strong>{t("usageByModel", lang)}</strong>
            {r.models.length === 0 ? <p className="map-muted">{t("usageEmpty", lang)}</p> : (
              <table className="usage-table">
                <thead>
                  <tr>
                    <th>{t("usageModel", lang)}</th>
                    <th className="num">{t("usageCalls", lang)}</th>
                    <th className="num">{t("usageSent", lang)}</th>
                    <th className="num">{t("usageRecv", lang)}</th>
                    <th className="num">{t("usageTheirs", lang)}</th>
                  </tr>
                </thead>
                <tbody>
                  {r.models.map((m) => (
                    <tr key={`${m.provider}/${m.model}`}>
                      <td><span className="usage-prov">{m.provider || "—"}</span> {m.model || "—"}</td>
                      <td className="num">{m.calls}</td>
                      <td className="num">{compact(m.sentTokens)}</td>
                      <td className="num">{compact(m.recvTokens)}</td>
                      <td className="num" title={m.reportedCalls ? `${m.reportedCalls}/${m.calls} ${t("usageReportedCalls", lang)}` : undefined}>
                        {m.reportedCalls ? compact(m.reportedIn + m.reportedOut) : t("usageNotReported", lang)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        </>
      )}
    </div>
  );
}

function Tile({ label, value, sub, strong, dot }: { label: string; value: string; sub?: string; strong?: boolean; dot?: "sent" | "recv" }) {
  return (
    <div className={`usage-tile${strong ? " strong" : ""}`}>
      <span className="usage-tile-label">{dot && <i className={`usage-key ${dot}`} />}{label}</span>
      <span className="usage-tile-value">{value}</span>
      {sub && <span className="usage-tile-sub">{sub}</span>}
    </div>
  );
}

// Compare sets the providers' word against Rove's count over the very calls
// they reported on, and says what the gap usually is.
function Compare({ t: x, lang }: { t: Tally; lang: Lang }) {
  if (x.calls === 0) return null;
  if (x.reportedCalls === 0) {
    return <div className="usage-compare none">{t("usageNoReports", lang)}</div>;
  }
  const theirs = x.reportedIn + x.reportedOut;
  const ours = x.oursOnReported;
  const diff = ours > 0 ? Math.round(((theirs - ours) / ours) * 100) : 0;
  const tone = Math.abs(diff) < 15 ? "close" : diff > 0 ? "more" : "less";
  return (
    <div className={`usage-compare ${tone}`}>
      <div className="usage-compare-nums">
        <span><b>{compact(ours)}</b> {t("usageOurCount", lang)}</span>
        <span className="usage-vs">↔</span>
        <span><b>{compact(theirs)}</b> {t("usageTheirCount", lang)}</span>
        <span className="usage-diff">{diff > 0 ? "+" : ""}{diff}%</span>
      </div>
      <p>
        {t(`usageGap_${tone}`, lang)}
        {x.cached > 0 && ` ${t("usageCached", lang).replace("{n}", compact(x.cached))}`}
        {x.reportedCalls < x.calls && ` ${t("usagePartial", lang).replace("{a}", String(x.reportedCalls)).replace("{b}", String(x.calls))}`}
      </p>
    </div>
  );
}

const CH = 180; // plot height
const PAD_L = 44;
const PAD_B = 22;

// DayChart is one stacked bar a day — sent below, received on top — on a
// single token axis, with a tooltip per day.
function DayChart({ r, lang }: { r: UsageReport; lang: Lang }) {
  const [hover, setHover] = useState<number | null>(null);
  const [w, setW] = useState(640);
  const ref = useCallback((el: HTMLDivElement | null) => {
    if (!el) return;
    const fit = () => setW(Math.max(280, el.clientWidth));
    fit();
    if (typeof ResizeObserver !== "undefined") new ResizeObserver(fit).observe(el);
  }, []);
  const max = Math.max(1, ...r.days.map((d) => d.sentTokens + d.recvTokens));
  const ticks = useMemo(() => niceTicks(max), [max]);
  const top = ticks[ticks.length - 1];
  const n = r.days.length;
  const plotW = w - PAD_L - 8;
  const slot = plotW / n;
  const barW = Math.max(2, Math.min(28, slot - Math.max(2, slot * 0.28)));
  const y = (v: number) => CH - (v / top) * CH;
  const every = n <= 10 ? 1 : n <= 31 ? 7 : 15;
  const h = hover !== null ? r.days[hover] : null;
  return (
    <div className="usage-chart" ref={ref} onMouseLeave={() => setHover(null)}>
      <svg width={w} height={CH + PAD_B + 8} role="img" aria-label={t("usageByDay", lang)}>
        <g transform="translate(0,6)">
          {ticks.map((v) => (
            <g key={v}>
              <line x1={PAD_L} x2={w - 8} y1={y(v)} y2={y(v)} className={v === 0 ? "usage-base" : "usage-grid"} />
              <text x={PAD_L - 8} y={y(v)} dy="0.32em" textAnchor="end" className="usage-axis">{compact(v)}</text>
            </g>
          ))}
          {r.days.map((d, i) => {
            const x = PAD_L + i * slot + (slot - barW) / 2;
            const sentH = (d.sentTokens / top) * CH;
            const recvH = (d.recvTokens / top) * CH;
            const gap = sentH > 0 && recvH > 0 ? 2 : 0;
            return (
              <g key={d.date} className={hover === i ? "on" : ""}>
                {sentH > 0 && <path d={bar(x, CH - sentH, barW, sentH, recvH === 0)} className="usage-bar sent" />}
                {recvH > 0 && <path d={bar(x, CH - sentH - recvH - gap, barW, Math.max(0, recvH), true)} className="usage-bar recv" />}
                {/* the hit target is the whole day's column, not the bar */}
                <rect x={PAD_L + i * slot} y={0} width={slot} height={CH} className="usage-hit" onMouseEnter={() => setHover(i)} />
                {(i % every === 0 || i === n - 1) && (
                  <text x={PAD_L + i * slot + slot / 2} y={CH + 15} textAnchor="middle" className="usage-axis">{d.date.slice(5)}</text>
                )}
              </g>
            );
          })}
        </g>
      </svg>
      {h && hover !== null && (
        <div className="usage-tip" style={{ left: Math.min(w - 190, Math.max(0, PAD_L + hover * slot + slot / 2 - 90)) }}>
          <strong>{h.date}</strong>
          <span><i className="usage-key sent" />{t("usageSent", lang)} <b>{compact(h.sentTokens)}</b></span>
          <span><i className="usage-key recv" />{t("usageRecv", lang)} <b>{compact(h.recvTokens)}</b></span>
          <span>{t("usageCalls", lang)} <b>{h.calls}</b></span>
          {h.reportedCalls > 0 && <span className="usage-tip-theirs">{t("usageTheirCount", lang)} <b>{compact(h.reportedIn + h.reportedOut)}</b></span>}
        </div>
      )}
    </div>
  );
}

function DayTable({ r, lang }: { r: UsageReport; lang: Lang }) {
  return (
    <table className="usage-table">
      <thead>
        <tr>
          <th>{t("usageDate", lang)}</th>
          <th className="num">{t("usageCalls", lang)}</th>
          <th className="num">{t("usageSent", lang)}</th>
          <th className="num">{t("usageRecv", lang)}</th>
          <th className="num">{t("usageTheirs", lang)}</th>
        </tr>
      </thead>
      <tbody>
        {[...r.days].reverse().map((d) => (
          <tr key={d.date} className={d.calls === 0 ? "quiet" : ""}>
            <td>{d.date}</td>
            <td className="num">{d.calls}</td>
            <td className="num">{compact(d.sentTokens)}</td>
            <td className="num">{compact(d.recvTokens)}</td>
            <td className="num">{d.reportedCalls ? compact(d.reportedIn + d.reportedOut) : "—"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// bar is a column with 4px rounded corners at its data end (the top) only,
// squared where it meets the baseline or the segment below.
function bar(x: number, y: number, w: number, h: number, roundTop: boolean): string {
  const r = roundTop ? Math.min(4, w / 2, h) : 0;
  return `M${x},${y + h} V${y + r} Q${x},${y} ${x + r},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${y + h} Z`;
}

// niceTicks gives 0 and three or four round steps up past max.
export function niceTicks(max: number): number[] {
  const raw = max / 4;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag;
  const out: number[] = [];
  for (let v = 0; v < max + step; v += step) out.push(v);
  return out;
}
