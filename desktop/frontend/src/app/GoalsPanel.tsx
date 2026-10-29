import { useCallback, useEffect, useState } from "react";
import { onReconnect, rpc, subscribeEvents } from "~/lib/rpc";
import { pollWhileVisible } from "~/lib/poll";
import { toast } from "~/lib/toast";
import { t, type Lang } from "~/lib/i18n";
import { canDrive, changes, has, toggle, tokens } from "~/lib/harness";
import { usePrefs } from "~/hooks/usePrefs";
import type { Goal, HarnessCategory, HarnessCategoryKey, HarnessView, JudgeVerdict, Session, Workspace } from "~/lib/types";

interface Props {
  workspace?: Workspace | null;
  // the chat open behind Settings: a goal made before goals had chats of
  // their own runs there
  sessionId?: string;
  // the list changed (the settings rail counts goals)
  onChange?: () => void;
}

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));
const clip = (s: string, n: number) => (s.length > n ? `${s.slice(0, n - 1)}…` : s);

// Goals: long jobs that run until the reviewer and their checks agree. Each
// shows where it stands, can be run, stopped and resumed, and opens onto its
// harness — the tactics it runs with, what they come to, how the harness
// changed them when the goal got stuck, and the verdict on every iteration.
// Everything follows the daemon's events, with a slow poll behind them.
export function GoalsPanel({ workspace, sessionId, onChange }: Props) {
  const { lang } = usePrefs();
  const [goals, setGoals] = useState<Goal[] | null>(null);
  const [catalog, setCatalog] = useState<HarnessCategory[]>([]);
  const [open, setOpen] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [sure, setSure] = useState<string | null>(null);
  const [form, setForm] = useState({ title: "", criteria: "", maxIterations: 8, check: "" });
  const [creating, setCreating] = useState(false);

  const reload = useCallback(async () => {
    try {
      const list = await rpc<Goal[]>("goal.list");
      setGoals(Array.isArray(list) ? list : []);
    } catch {
      setGoals((g) => g ?? []);
    }
  }, []);

  useEffect(() => {
    void rpc<HarnessCategory[]>("harness.catalog")
      .then((c) => setCatalog(Array.isArray(c) ? c : []))
      .catch(() => setCatalog([])); // an older daemon: no harness to show
  }, []);

  useEffect(() => {
    const stop = pollWhileVisible(() => void reload(), 15000);
    // a drive sends a burst (status, verdict, harness); one read after it
    let soon: ReturnType<typeof setTimeout> | null = null;
    const later = () => {
      if (!soon) soon = setTimeout(() => { soon = null; void reload(); }, 250);
    };
    const offs = [
      subscribeEvents("goal.updated", later),
      subscribeEvents("judge.verdict", later),
      onReconnect(() => void reload()),
    ];
    return () => {
      stop();
      if (soon) clearTimeout(soon);
      offs.forEach((f) => f());
    };
  }, [reload]);

  useEffect(() => { if (goals) onChange?.(); }, [goals, onChange]);

  // the delete button asks once; the question goes away on its own
  useEffect(() => {
    if (!sure) return;
    const id = window.setTimeout(() => setSure(null), 3000);
    return () => window.clearTimeout(id);
  }, [sure]);

  const act = async (g: Goal, what: () => Promise<unknown>, done?: string) => {
    setBusy(g.id);
    try {
      await what();
      if (done) toast(`${g.title}: ${done}`, "ok");
    } catch (e) {
      toast(msg(e), "err");
    } finally {
      setBusy(null);
      void reload();
    }
  };

  const drive = (g: Goal) => act(g, () => rpc("goal.drive", {
    id: g.id,
    // its own chat; one made before goals had chats runs in the open one
    sessionId: g.sessionId || sessionId || "",
    // its own workspace wins; the open one is only for a goal without
    workspace: g.workspaceId ? "" : (workspace?.path ?? ""),
  }), t("goalStarted", lang));
  const stop = (g: Goal) => act(g, () => rpc("goal.cancel", { id: g.id }), t("goalStopped", lang));
  const remove = (g: Goal) => {
    if (sure !== g.id) { setSure(g.id); return; }
    setSure(null);
    if (open === g.id) setOpen(null);
    void act(g, () => rpc("goal.delete", { id: g.id }));
  };

  const create = async () => {
    const title = form.title.trim();
    if (!title) { toast(t("goalNeedsTitle", lang), "err"); return; }
    setCreating(true);
    try {
      // a goal works in a chat (as /goal does): this one gets its own
      const chat = await rpc<Session>("session.create", {
        title, space: "chat", mode: "orchestra", workspaceId: workspace?.id ?? "",
      });
      const check = form.check.trim();
      await rpc("goal.create", {
        title,
        sessionId: chat.id,
        workspaceId: workspace?.id ?? chat.workspaceId ?? "",
        completionContract: {
          criteria: form.criteria.split(",").map((s) => s.trim()).filter(Boolean),
          qualityGates: check ? [{ name: "check", kind: "custom", command: check, expectExitZero: true }] : [],
          maxIterations: Math.min(50, Math.max(1, Math.round(form.maxIterations) || 8)),
        },
      });
      window.dispatchEvent(new Event("rove:sessions"));
      setForm({ title: "", criteria: "", maxIterations: 8, check: "" });
      await reload();
    } catch (e) {
      toast(msg(e), "err");
    } finally {
      setCreating(false);
    }
  };

  return (
    <>
      <h2>{t("goals", lang)}</h2>
      <p className="split-lead">{t("goalsLead", lang)}</p>
      {goals && goals.length === 0 ? (
        <div className="empty">
          <strong>{t("goalsEmpty", lang)}</strong>
          <p>{t("goalsEmptyHint", lang)}</p>
        </div>
      ) : (
        <div className="row-list goal-list" style={{ marginBottom: 18 }}>
          {(goals ?? []).map((g) => {
            const running = g.status === "running";
            const max = g.completionContract?.maxIterations ?? 0;
            const reason = g.lastVerdict?.reason ?? "";
            return (
              <div key={g.id} className="goal-item">
                <div className="row goal-row">
                  <span className={`pip goal-pip ${g.status}${running ? " run" : g.status === "done" ? " on" : ""}`} />
                  <div className="meta">
                    <strong>{g.title}</strong>
                    <span>
                      <span className={`goal-status ${g.status}`}>{t(`goalStatus_${g.status}`, lang)}</span>
                      {" · "}
                      {t("goalIter", lang).replace("{n}", String(g.iteration)).replace("{max}", max ? String(max) : "∞")}
                      {reason && <span className="goal-reason" title={reason}>{" · "}{t("goalReviewer", lang)}: {clip(reason, 140)}</span>}
                    </span>
                  </div>
                  {running ? (
                    <button className="ghost" disabled={busy === g.id} onClick={() => void stop(g)}>■ {t("goalStop", lang)}</button>
                  ) : canDrive(g) ? (
                    <button className="primary" disabled={busy === g.id} onClick={() => void drive(g)}>
                      ▶ {t(g.iteration > 0 ? "goalResume" : "goalRun", lang)}
                    </button>
                  ) : null}
                  <button
                    className="ghost"
                    aria-expanded={open === g.id}
                    onClick={() => setOpen(open === g.id ? null : g.id)}
                  >
                    {t("goalDetails", lang)} {open === g.id ? "▴" : "▾"}
                  </button>
                  <button className="danger-btn" disabled={busy === g.id} onClick={() => remove(g)}>
                    {sure === g.id ? t("goalDeleteSure", lang) : t("delete", lang)}
                  </button>
                </div>
                {open === g.id && <GoalDetail goal={g} catalog={catalog} lang={lang} />}
              </div>
            );
          })}
        </div>
      )}
      <div className="form-stack">
        <label htmlFor="goal-title">{t("goalTitle", lang)}</label>
        <input id="goal-title" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder={t("goalTitleHint", lang)} />
        <label htmlFor="goal-criteria">{t("goalCriteria", lang)}</label>
        <input id="goal-criteria" value={form.criteria} onChange={(e) => setForm({ ...form, criteria: e.target.value })} placeholder={t("goalCriteriaHint", lang)} />
        <label htmlFor="goal-max">{t("goalMaxIter", lang)}</label>
        <input
          id="goal-max"
          type="number"
          min={1}
          max={50}
          value={form.maxIterations}
          onChange={(e) => setForm({ ...form, maxIterations: Number(e.target.value) })}
        />
        <label htmlFor="goal-check">{t("goalCheck", lang)}</label>
        <input id="goal-check" value={form.check} onChange={(e) => setForm({ ...form, check: e.target.value })} placeholder={t("goalCheckHint", lang)} spellCheck={false} />
        <button className="primary" disabled={creating} onClick={() => void create()}>{t("create", lang)}</button>
      </div>
    </>
  );
}

// the recovery plan's actions, by the name of the tactic behind each
const ACTION_TACTIC: Record<string, string> = {
  retry: "retry", checkpoint_resume: "checkpointResume", replan: "replan",
  context_reset: "contextReset", model_fallback: "modelFallback", escalate: "escalation",
};

function GoalDetail({ goal, catalog, lang }: { goal: Goal; catalog: HarnessCategory[]; lang: Lang }) {
  const [view, setView] = useState<HarnessView | null>(null);
  const [verdicts, setVerdicts] = useState<JudgeVerdict[]>([]);
  const [err, setErr] = useState("");
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      const [v, vs] = await Promise.all([
        rpc<HarnessView>("harness.get", { goalId: goal.id }),
        rpc<JudgeVerdict[]>("goal.verdicts", { id: goal.id }),
      ]);
      setView(v);
      setVerdicts(Array.isArray(vs) ? vs : []);
      setErr("");
    } catch (e) {
      setErr(msg(e));
    }
  }, [goal.id]);

  // a new status or iteration (the list follows the goal's events) is a
  // new state of its harness too
  useEffect(() => { void load(); }, [load, goal.status, goal.iteration]);
  useEffect(() => subscribeEvents("harness.updated", (ev) => {
    if ((ev.payload as { goalId?: string } | undefined)?.goalId === goal.id) void load();
  }), [goal.id, load]);

  const change = async (method: "harness.set" | "harness.compose", params: object) => {
    setSaving(true);
    try {
      setView(await rpc<HarnessView>(method, { goalId: goal.id, ...params }));
    } catch (e) {
      toast(msg(e), "err");
    } finally {
      setSaving(false);
    }
  };
  const pick = (cat: HarnessCategoryKey, bit: number) => {
    if (view && !view.running && !saving) void change("harness.set", { profile: toggle(view.profile, cat, bit) });
  };

  if (err) return <div className="goal-detail"><p className="danger">{err}</p></div>;
  if (!view) return <div className="goal-detail"><p className="harness-hint">…</p></div>;
  const locked = view.running || saving;
  const r = view.resolved;
  const effect = [
    t("harnessTurns", lang).replace("{n}", String(r.maxTurns)),
    t("harnessBudget", lang).replace("{n}", tokens(r.contextBudget)),
    r.allowedTools ? t("harnessToolsN", lang).replace("{n}", String(r.allowedTools.length)) : t("harnessAllTools", lang),
    ...(r.toolConcurrency > 1 ? [t("harnessParallel", lang).replace("{n}", String(r.toolConcurrency))] : []),
  ];
  const recovery = r.recovery.map((a) => (ACTION_TACTIC[a] ? t(`harnessTac_${ACTION_TACTIC[a]}`, lang) : "—"));
  const mutations = [...(view.mutations ?? [])].reverse();
  const progress = view.checkpoint?.progress ?? [];

  return (
    <div className="goal-detail">
      <div className="harness-head">
        <strong>{t("harnessTitle", lang)}</strong>
        <span className={`harness-mode ${view.profile.mode || "auto"}`}>
          {t(view.profile.mode === "manual" ? "harnessManual" : "harnessAuto", lang)} · v{view.profile.version}
        </span>
        <button className="ghost" disabled={locked} onClick={() => void change("harness.compose", {})}>
          {t("harnessCompose", lang)}
        </button>
      </div>
      <p className="harness-hint">{view.running ? t("harnessLocked", lang) : t("harnessPickHint", lang)}</p>
      <div className="tactic-grid">
        {catalog.map((c) => (
          <div key={c.key} className="tactic-row">
            <span className="tactic-cat">{t(`harnessCat_${c.key}`, lang)}</span>
            <div className="tactic-chips">
              {c.tactics.map((tc) => {
                const on = has(view.profile, c.key, tc.bit);
                return (
                  <button
                    key={tc.key}
                    type="button"
                    className={`tactic${on ? " on" : ""}`}
                    aria-pressed={on}
                    title={t(`harnessHint_${tc.key}`, lang)}
                    disabled={locked}
                    onClick={() => pick(c.key, tc.bit)}
                  >
                    {t(`harnessTac_${tc.key}`, lang)}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>
      <div className="harness-effect">
        <span className="tactic-cat">{t("harnessEffective", lang)}</span>
        <span>{effect.join(" · ")}</span>
      </div>
      <div className="harness-effect">
        <span className="tactic-cat">{t("harnessRecovery", lang)}</span>
        <span>{recovery.join(" → ")}</span>
      </div>

      {view.checkpoint && (
        <section className="harness-section">
          <h4>
            {t("harnessCheckpoint", lang)}
            <span className="harness-sub">
              {t("harnessCheckpointAt", lang)
                .replace("{n}", String(view.checkpoint.iteration))
                .replace("{at}", new Date(view.checkpoint.savedAt).toLocaleString())}
            </span>
          </h4>
          {progress.length > 0 && <ul className="harness-progress">{progress.map((p, i) => <li key={i}>{p}</li>)}</ul>}
        </section>
      )}

      <section className="harness-section">
        <h4>{t("harnessChanges", lang)}</h4>
        {mutations.length === 0 ? (
          <p className="harness-hint">{t("harnessNoChanges", lang)}</p>
        ) : (
          <ol className="mutation-list">
            {mutations.map((m, i) => (
              <li key={`${m.at}-${i}`}>
                <span className="mutation-it">#{m.iteration}</span>
                <span className="mutation-reason">
                  {m.reason === "set by hand" ? t("harnessByHand", lang) : m.reason === "composed again" ? t("harnessComposedAgain", lang) : m.reason}
                </span>
                <span className="mutation-diff">
                  {changes(catalog, m.old, m.new).map((ch) => (
                    <span key={`${ch.cat}-${ch.key}`} className={ch.on ? "plus" : "minus"}>
                      {ch.on ? "+" : "−"}{t(`harnessTac_${ch.key}`, lang)}
                    </span>
                  ))}
                </span>
              </li>
            ))}
          </ol>
        )}
      </section>

      <section className="harness-section">
        <h4>{t("goalVerdicts", lang)}</h4>
        {verdicts.length === 0 ? (
          <p className="harness-hint">{t("goalNoVerdicts", lang)}</p>
        ) : (
          <ol className="verdict-list">
            {[...verdicts].reverse().map((v) => {
              const failed = (v.gateResults ?? []).filter((x) => !x.passed);
              return (
                <li key={v.id}>
                  <span className="mutation-it">#{v.iteration}</span>
                  <span className={`verdict ${v.decision.toLowerCase()}`}>{v.decision}</span>
                  <span className="mutation-reason">{v.reason}</span>
                  {failed.length > 0 && <span className="verdict-gates">✗ {failed.map((x) => x.name).join(", ")}</span>}
                </li>
              );
            })}
          </ol>
        )}
      </section>
    </div>
  );
}
