import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { AutomationJob } from "~/lib/types";
import { toast } from "~/lib/toast";
import { t } from "~/lib/i18n";
import { usePrefs } from "~/hooks/usePrefs";

const KINDS: { id: AutomationJob["kind"]; label: string }[] = [
  { id: "sweep_ready", label: "Sweep ready → run" },
  { id: "assign_idle", label: "Assign idle agents" },
  { id: "drive_goals", label: "Drive pending goals" },
];

export function Automation() {
  const { lang } = usePrefs();
  const [jobs, setJobs] = useState<AutomationJob[]>([]);
  const [draft, setDraft] = useState({ name: "", kind: "sweep_ready" as string, everySeconds: 30 });

  const load = useCallback(async () => {
    try {
      setJobs((await rpc<AutomationJob[]>("automation.list")) ?? []);
    } catch {
      setJobs([]);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const save = async (j: Partial<AutomationJob> & { kind: string }) => {
    try {
      await rpc("automation.upsert", j);
      await load();
      toast(j.name || j.kind, "ok");
    } catch (e) {
      toast(e instanceof Error ? e.message : "automation failed", "err");
    }
  };

  return (
    <>
      <h2>{t("automation", lang)}</h2>
      <p className="split-lead">{t("autoLead", lang)}</p>
      <div className="row-list">
        {jobs.map((j) => (
          <div key={j.id} className="row">
            <div className="meta">
              <strong>{j.name || j.kind}</strong>
              <span>
                {j.kind} · {j.everySeconds}s
                {j.lastResult ? ` · ${j.lastResult}` : ""}
              </span>
            </div>
            <button
              className="ghost"
              style={{ fontSize: 11 }}
              onClick={() => void save({ ...j, enabled: !j.enabled })}
            >
              {j.enabled ? "on" : t("disabled", lang)}
            </button>
            <button
              className="ghost"
              style={{ fontSize: 11 }}
              onClick={() => void rpc("automation.tick").then(() => load()).then(() => toast("tick", "ok"))}
            >
              ▶
            </button>
            <button
              className="danger-btn"
              style={{ fontSize: 11 }}
              onClick={() => void rpc("automation.delete", { id: j.id }).then(() => load())}
            >
              {t("delete", lang)}
            </button>
          </div>
        ))}
      </div>
      <div style={{ display: "flex", gap: 6, marginTop: 14, flexWrap: "wrap" }}>
        <input
          placeholder={t("create", lang)}
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          style={{ flex: 1, minWidth: 120 }}
        />
        <select value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}>
          {KINDS.map((k) => <option key={k.id} value={k.id}>{k.label}</option>)}
        </select>
        <input
          type="number"
          min={5}
          value={draft.everySeconds}
          onChange={(e) => setDraft({ ...draft, everySeconds: Number(e.target.value) || 30 })}
          style={{ width: 72 }}
        />
        <button
          className="primary"
          onClick={() => void save({
            name: draft.name || draft.kind,
            kind: draft.kind,
            everySeconds: draft.everySeconds,
            enabled: true,
          })}
        >
          {t("save", lang)}
        </button>
      </div>
    </>
  );
}
