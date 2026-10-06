import { useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { MonitorPreset, StaffHandoff, StaffMember, StaffMonitor, StaffSchedule, StaffTask, StaffWatch, Workspace } from "~/lib/types";
import { useStaff } from "~/hooks/useStaff";
import { usePrefs } from "~/hooks/usePrefs";
import { t, type Lang } from "~/lib/i18n";
import { toast } from "~/lib/toast";
import { AgentMark } from "./AgentMark";
import { Icon } from "./Icons";
import { ago } from "./SessionSidebar";

interface Props {
  workspace: Workspace | null;
  // a task's or a live chat's conversation
  onOpenSession: (sessionId: string) => void;
  // a new conversation with an agent
  onTalk: (profileId: string) => void;
  onEdit: (profileId: string) => void;
  onAdd: () => void;
}

// StaffBoard is the Agent space's home: the user's agents as a team. Each
// card says who the agent is (its title), whether it is working, has a
// report waiting or is free, and what it is on; a task can be handed to it
// right there — it works in the background and reports back — and its
// notes, what it remembers across conversations, can be read and pruned.
export function StaffBoard({ workspace, onOpenSession, onTalk, onEdit, onAdd }: Props) {
  const { lang } = usePrefs();
  const { members, watches, schedules, monitors, handoffs, chats, reload } = useStaff();
  const nameOf = (id: string) => members?.find((m) => m.profile.id === id)?.profile.name ?? "?";
  if (members === null) return <div className="staff" />;
  const working = members.filter((m) => m.state === "working").length;
  const reports = members.filter((m) => m.state === "report").length;
  return (
    <div className="staff">
      <header className="staff-head">
        <div>
          <h1>{t("staffTitle", lang)}</h1>
          <p>{t("staffLead", lang)}</p>
        </div>
        <div className="staff-counts">
          {working > 0 && <span className="staff-chip working"><span className="staff-dot working" />{working} {t("staffWorkingN", lang)}</span>}
          {reports > 0 && <span className="staff-chip report"><span className="staff-dot report" />{reports} {t("staffReportsN", lang)}</span>}
          <button type="button" className="primary" onClick={onAdd}><Icon name="plus" size={13} /> {t("ofNew", lang)}</button>
        </div>
      </header>
      {members.length === 0 ? (
        <div className="staff-empty">
          <Icon name="agents" size={28} />
          <strong>{t("ofEmpty", lang)}</strong>
          <p>{t("ofEmptyHint", lang)}</p>
        </div>
      ) : (
        <div className="staff-grid">
          {members.map((m) => (
            <StaffCard key={m.profile.id} m={m} lang={lang} workspace={workspace} onChanged={reload}
              watches={watches.filter((w) => w.profileId === m.profile.id)} schedules={schedules.filter((x) => x.profileId === m.profile.id)} chats={chats}
              monitors={monitors.filter((x) => x.profileId === m.profile.id)}
              handoffs={handoffs.filter((x) => x.fromId === m.profile.id || x.toId === m.profile.id)} nameOf={nameOf}
              onOpenSession={onOpenSession} onTalk={onTalk} onEdit={onEdit} />
          ))}
        </div>
      )}
    </div>
  );
}

function StaffCard({ m, lang, workspace, onChanged, onOpenSession, onTalk, onEdit, watches, schedules, chats, monitors, handoffs, nameOf }: {
  monitors: StaffMonitor[];
  handoffs: StaffHandoff[];
  nameOf: (id: string) => string;
  m: StaffMember;
  watches: StaffWatch[];
  schedules: StaffSchedule[];
  chats: Map<string, string>;
  lang: Lang;
  workspace: Workspace | null;
  onChanged: () => Promise<void>;
  onOpenSession: (sessionId: string) => void;
  onTalk: (profileId: string) => void;
  onEdit: (profileId: string) => void;
}) {
  const p = m.profile;
  const [task, setTask] = useState("");
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const [notesOpen, setNotesOpen] = useState(false);
  const [note, setNote] = useState("");
  const [addingMonitor, setAddingMonitor] = useState(false);

  const assign = async () => {
    const text = task.trim();
    if (!text || busy) return;
    setBusy(true);
    try {
      await rpc("staff.assign", { profileId: p.id, task: text, workspaceId: workspace?.id ?? "" });
      setTask("");
      toast(`${p.name}: ${t("staffAssigned", lang)}`, "ok");
      window.dispatchEvent(new Event("rove:sessions"));
      await onChanged();
    } catch (e) {
      toast(e instanceof Error ? e.message : "assign failed", "err");
    } finally {
      setBusy(false);
    }
  };

  // reading a report marks it read
  const toggle = async (x: StaffTask) => {
    setOpen((cur) => (cur === x.id ? null : x.id));
    if (x.status !== "running" && !x.seen) {
      await rpc("staff.seen", { id: x.id }).catch(() => {});
      await onChanged();
    }
  };

  const stop = async (x: StaffTask) => {
    await rpc("staff.stop", { id: x.id }).catch((e) => toast(e instanceof Error ? e.message : "stop", "err"));
    await onChanged();
  };

  const addNote = async () => {
    const text = note.trim();
    if (!text) return;
    await rpc("staff.noteAdd", { profileId: p.id, note: text }).catch((e) => toast(e instanceof Error ? e.message : "note", "err"));
    setNote("");
    await onChanged();
  };
  const dropNote = async (id: string) => {
    await rpc("staff.noteDelete", { id }).catch(() => {});
    await onChanged();
  };

  return (
    <article className={`staff-card ${m.state}`} data-agent={p.id}>
      <div className="staff-card-top">
        <AgentMark mark={p.mark} color={p.color} seed={p.id} character={p.characterId} size={38} />
        <div className="staff-who">
          <strong>{p.name}</strong>
          <span>{p.title || t("staffNoTitle", lang)}</span>
        </div>
        <span className={`staff-chip ${m.state}`}><span className={`staff-dot ${m.state}`} />{t(`staffState_${m.state}`, lang)}</span>
        <button type="button" className="tree-icon" title={t("ofEditAgent", lang)} onClick={() => onEdit(p.id)}><Icon name="sliders" size={13} /></button>
      </div>

      {m.state === "working" && m.now && (
        <button type="button" className="staff-now" title={t("staffOpenChat", lang)} onClick={() => m.nowSession && onOpenSession(m.nowSession)}>
          <span className="staff-spin" aria-hidden />
          <span className="staff-now-text">{m.now}</span>
          <span className="staff-now-go" aria-hidden>›</span>
        </button>
      )}

      <form className="staff-assign" onSubmit={(e) => { e.preventDefault(); void assign(); }}>
        <textarea
          rows={2}
          value={task}
          placeholder={t("staffAssignPh", lang).replace("{name}", p.name)}
          onChange={(e) => setTask(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void assign(); } }}
          disabled={busy}
        />
        <button type="submit" className="primary" disabled={!task.trim() || busy}>{t("staffAssign", lang)}</button>
      </form>

      {m.tasks.length > 0 && (
        <ul className="staff-tasks" aria-label={t("staffTasks", lang)}>
          {m.tasks.map((x) => (
            <li key={x.id} className={`staff-task ${x.status}${!x.seen && x.status !== "running" ? " unseen" : ""}${open === x.id ? " open" : ""}`}>
              <button type="button" className="staff-task-row" onClick={() => void toggle(x)}>
                <span className={`staff-dot ${x.status}`} />
                <span className="staff-task-title" title={x.brief}>{x.title}</span>
                {x.fromName && <span className="staff-from" title={t("staffFromHint", lang)}>← {x.fromName}</span>}
                {x.origin && !x.scheduleId && !x.handoffId && <span className="staff-from staff-watch" title={t("staffWatchHint", lang)}>◉ {x.origin}</span>}
                {x.handoffId && <span className="staff-from staff-watch" title={t("staffTakesFrom", lang).replace("{name}", x.origin ?? "")}>⇠ {x.origin}</span>}
                {x.scheduleId && <span className="staff-from staff-watch" title={t("staffScheduleHint", lang)}>⏱ {scheduleLabel(schedules.find((y) => y.id === x.scheduleId), lang) ?? x.origin}</span>}
                {x.isolation?.state === "review" && <span className="staff-review-chip">{t("staffIsoReviewChip", lang)}</span>}
                <span className="staff-ago">{ago(x.endedAt || x.createdAt, lang)}</span>
              </button>
              {open === x.id && (
                <div className="staff-report">
                  {x.status === "running" ? <p className="map-muted">{t("staffStillWorking", lang)}</p> : (
                    <pre>{x.report || x.error || t("staffNoReport", lang)}</pre>
                  )}
                  {x.isolation && <Changes task={x} lang={lang} onChanged={onChanged} />}
                  <div className="staff-report-acts">
                    <button type="button" className="ghost" onClick={() => onOpenSession(x.sessionId)}>{t("staffOpenChat", lang)}</button>
                    {x.status === "running" && <button type="button" className="ghost" onClick={() => void stop(x)}>{t("stop", lang)}</button>}
                  </div>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {(watches.length > 0 || schedules.length > 0 || monitors.length > 0 || handoffs.length > 0) && (
        <ul className="staff-autos" aria-label={t("staffAutos", lang)}>
          {handoffs.map((h) => {
            const out = h.fromId === p.id;
            return (
              <AutoRow key={h.id} on={h.enabled} icon={out ? "⇢" : "⇠"} lang={lang}
                label={t(out ? "staffHandsTo" : "staffTakesFrom", lang).replace("{name}", nameOf(out ? h.toId : h.fromId))}
                detail={h.instruction}
                onToggle={() => void rpc("staff.handoffSave", { ...h, enabled: !h.enabled }).then(onChanged)}
                onRemove={() => void rpc("staff.handoffDelete", { id: h.id }).then(onChanged)} />
            );
          })}
          {monitors.map((x) => (
            <AutoRow key={x.id} on={x.enabled} icon="◎" lang={lang}
              label={`${x.name} · ${t("staffEvery", lang).replace("{n}", every(x.everyMinutes, lang))}`}
              detail={`${x.command}\n\n${x.instruction}`} error={x.lastError}
              onToggle={() => void rpc("staff.monitorSave", { ...x, enabled: !x.enabled }).then(onChanged)}
              onRemove={() => void rpc("staff.monitorDelete", { id: x.id }).then(onChanged)} />
          ))}
          {watches.map((w) => (
            <AutoRow key={w.id} on={w.enabled} icon="◉" lang={lang}
              label={t("staffWatchesChat", lang).replace("{chat}", chats.get(w.sessionId) ?? "?")} detail={w.instruction}
              onToggle={() => void rpc("staff.watchSave", { ...w, enabled: !w.enabled }).then(onChanged)}
              onRemove={() => void rpc("staff.watchDelete", { id: w.id }).then(onChanged)} />
          ))}
          {schedules.map((x) => (
            <AutoRow key={x.id} on={x.enabled} icon="⏱" lang={lang}
              label={scheduleLabel(x, lang) ?? ""}
              detail={x.instruction}
              onToggle={() => void rpc("staff.scheduleSave", { ...x, enabled: !x.enabled }).then(onChanged)}
              onRemove={() => void rpc("staff.scheduleDelete", { id: x.id }).then(onChanged)} />
          ))}
        </ul>
      )}

      {addingMonitor && (
        <MonitorForm profileId={p.id} name={p.name} workspace={workspace} lang={lang}
          onDone={async () => { setAddingMonitor(false); await onChanged(); }} onCancel={() => setAddingMonitor(false)} />
      )}

      <footer className="staff-foot">
        <button type="button" className={`ghost staff-notes-btn${notesOpen ? " on" : ""}`} aria-expanded={notesOpen} onClick={() => setNotesOpen((v) => !v)}>
          <Icon name="flag" size={12} /> {t("staffNotes", lang)} ({m.notes.length})
        </button>
        <button type="button" className={`ghost${addingMonitor ? " on" : ""}`} aria-expanded={addingMonitor} onClick={() => setAddingMonitor((v) => !v)} title={t("staffMonitorHint", lang)}>◎ {t("staffMonitorAdd", lang)}</button>
        <span className="tw-spacer" />
        <button type="button" className="ghost" onClick={() => onTalk(p.id)}><Icon name="chat" size={12} /> {t("staffTalk", lang)}</button>
      </footer>

      {notesOpen && (
        <div className="staff-notes">
          {m.notes.length === 0 && <p className="map-muted">{t("staffNotesEmpty", lang)}</p>}
          {m.notes.map((n) => (
            <div key={n.id} className={`staff-note${n.key.startsWith("learned:") ? " learned" : ""}`}>
              {n.key.startsWith("learned:") && <span className="staff-learned" title={t("staffLearnedHint", lang)}>{t("staffLearned", lang)}</span>}
              <span>{n.content}</span>
              <button type="button" className="tw-helper-x" title={t("delete", lang)} onClick={() => void dropNote(n.id)}>×</button>
            </div>
          ))}
          <form className="staff-note-add" onSubmit={(e) => { e.preventDefault(); void addNote(); }}>
            <input value={note} placeholder={t("staffNotePh", lang)} onChange={(e) => setNote(e.target.value)} />
          </form>
        </div>
      )}
    </article>
  );
}

// scheduleLabel says when a schedule runs, in the user's language.
function scheduleLabel(x: StaffSchedule | undefined, lang: Lang): string | undefined {
  if (!x) return undefined;
  return x.everyMinutes ? t("staffEvery", lang).replace("{n}", every(x.everyMinutes, lang)) : t("staffDailyAt", lang).replace("{t}", x.dailyAt ?? "");
}

// every says a repeat in the user's words: "3 saat", "45 dk".
function every(min: number, lang: Lang): string {
  return min % 60 === 0 ? `${min / 60} ${t("staffHours", lang)}` : `${min} ${t("staffMinutes", lang)}`;
}

// AutoRow is one automation on an agent's card: what sets it off, what it
// does (on hover), on/off, and a remove that asks twice.
function AutoRow({ on, icon, label, detail, error, lang, onToggle, onRemove }: {
  error?: string;
  on: boolean;
  icon: string;
  label: string;
  detail: string;
  lang: Lang;
  onToggle: () => void;
  onRemove: () => void;
}) {
  const [arm, setArm] = useState(false);
  return (
    <li className={`staff-auto${on ? "" : " off"}`}>
      <span className="staff-auto-icon" aria-hidden>{icon}</span>
      <span className="staff-auto-text" title={detail}>{label}{error && <span className="staff-auto-error" title={error}> · ⚠ {error}</span>}</span>
      <button type="button" className="ghost staff-auto-btn" aria-pressed={on} onClick={onToggle}>{t(on ? "ctxWatchOn" : "ctxWatchOff", lang)}</button>
      <button type="button" className={`tw-helper-x${arm ? " armed" : ""}`} title={t(arm ? "ctxWatchDeleteSure" : "delete", lang)}
        onBlur={() => setArm(false)} onClick={() => { if (arm) onRemove(); else setArm(true); }}>×</button>
    </li>
  );
}

// Changes is what a task did to the project, kept in its own checkout until
// the user decides: look at the diff, apply it, throw it away, or open it
// as a pull request.
function Changes({ task, lang, onChanged }: { task: StaffTask; lang: Lang; onChanged: () => Promise<void> }) {
  const iso = task.isolation!;
  const [diff, setDiff] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [arm, setArm] = useState(false);
  const act = async (method: string, ok: string) => {
    setBusy(true);
    try {
      await rpc(method, { id: task.id });
      toast(t(ok, lang), "ok");
    } catch (e) {
      toast(e instanceof Error ? e.message : method, "err");
    } finally {
      setBusy(false);
      await onChanged();
    }
  };
  const count = iso.files?.length ?? 0;
  const stats = count > 0 ? `${count} ${t("staffIsoFiles", lang)} · +${iso.added} −${iso.removed}` : "";
  return (
    <div className={`staff-changes ${iso.state}`}>
      <div className="staff-changes-head">
        <span className="staff-changes-state">{t(`staffIso_${iso.state}`, lang).replace("{name}", (iso.note ?? "").replace(/^carried on by /, ""))}</span>
        {stats && <span className="staff-changes-stats">{stats}</span>}
        {iso.state === "pr" && iso.pr && <a href={iso.pr} target="_blank" rel="noreferrer" className="staff-pr-link">{t("staffIsoOpenPR", lang)} ↗</a>}
      </div>
      {iso.note && iso.state !== "carried" && <p className="staff-changes-note">{iso.note}</p>}
      {iso.state === "review" && (
        <>
          {count > 0 && <div className="staff-changes-files" title={iso.files!.join("\n")}>{iso.files!.slice(0, 4).join(", ")}{count > 4 ? ` +${count - 4}` : ""}</div>}
          <div className="staff-report-acts">
            <button type="button" className="ghost" disabled={busy} onClick={async () => {
              if (diff !== null) { setDiff(null); return; }
              try { setDiff((await rpc<{ patch: string }>("staff.diff", { id: task.id })).patch); } catch (e) { toast(e instanceof Error ? e.message : "diff", "err"); }
            }}>{t(diff === null ? "staffIsoSeeDiff" : "staffIsoHideDiff", lang)}</button>
            <button type="button" className="primary" disabled={busy} onClick={() => void act("staff.apply", "staffIsoApplied")}>{t("staffIsoApply", lang)}</button>
            <button type="button" className="ghost" disabled={busy} onClick={() => void act("staff.pr", "staffIsoPROpened")}>{t("staffIsoPR", lang)}</button>
            <button type="button" className={`ghost danger${arm ? " armed" : ""}`} disabled={busy} onBlur={() => setArm(false)}
              onClick={() => { if (arm) void act("staff.discard", "staffIsoDiscarded"); else setArm(true); }}>
              {t(arm ? "staffIsoDiscardSure" : "staffIsoDiscard", lang)}
            </button>
          </div>
          {diff !== null && <pre className="staff-diff">{diff.split("\n").map((l, i) => (
            <span key={i} className={l.startsWith("+") && !l.startsWith("+++") ? "add" : l.startsWith("-") && !l.startsWith("---") ? "del" : l.startsWith("@@") ? "hunk" : ""}>{l}{"\n"}</span>
          ))}</pre>}
        </>
      )}
    </div>
  );
}

// MonitorForm sets up a monitor for an agent: a ready-made one (GitHub CI,
// new bugs, Sentry) or a command of the user's own, checked on a timer in
// the open project's folder.
function MonitorForm({ profileId, name, workspace, lang, onDone, onCancel }: {
  profileId: string;
  name: string;
  workspace: Workspace | null;
  lang: Lang;
  onDone: () => Promise<void>;
  onCancel: () => void;
}) {
  const [presets, setPresets] = useState<MonitorPreset[]>([]);
  const [form, setForm] = useState({ name: "", command: "", instruction: "", every: 15 });
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    rpc<MonitorPreset[]>("staff.monitorPresets").then((l) => setPresets(Array.isArray(l) ? l : [])).catch(() => {});
  }, []);
  const pick = (p: MonitorPreset) => setForm({ name: p.name, command: p.command, instruction: p.instruction, every: p.every });
  const save = async () => {
    if (!form.command.trim() || busy) return;
    setBusy(true);
    try {
      await rpc("staff.monitorSave", { profileId, name: form.name, command: form.command, instruction: form.instruction, everyMinutes: form.every, workspaceId: workspace?.id ?? "" });
      toast(`${name}: ${t("staffMonitorSaved", lang)}`, "ok");
      await onDone();
    } catch (e) {
      toast(e instanceof Error ? e.message : "monitor", "err");
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="staff-monitor-form" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <p className="staff-monitor-lead">{t("staffMonitorLead", lang).replace("{name}", name)}</p>
      <div className="staff-monitor-presets">
        {presets.map((p) => (
          <button key={p.key} type="button" className={`ghost${form.command === p.command ? " on" : ""}`} title={`${t("staffMonitorNeeds", lang)}: ${p.needs}`} onClick={() => pick(p)}>{p.name}</button>
        ))}
      </div>
      <input aria-label={t("staffMonitorName", lang)} placeholder={t("staffMonitorName", lang)} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
      <textarea aria-label={t("staffMonitorCommand", lang)} rows={2} placeholder={t("staffMonitorCommandPh", lang)} value={form.command} onChange={(e) => setForm({ ...form, command: e.target.value })} />
      <textarea aria-label={t("staffMonitorInstruction", lang)} rows={2} placeholder={t("staffMonitorInstruction", lang)} value={form.instruction} onChange={(e) => setForm({ ...form, instruction: e.target.value })} />
      <label className="staff-monitor-every">
        {t("staffMonitorEvery", lang)}
        <input type="number" min={5} max={1440} value={form.every} onChange={(e) => setForm({ ...form, every: Number(e.target.value) || 15 })} />
      </label>
      <p className="staff-monitor-note">{t("staffMonitorNote", lang)}</p>
      <div className="staff-report-acts">
        <button type="submit" className="primary" disabled={!form.command.trim() || busy}>{t("staffMonitorSave", lang)}</button>
        <button type="button" className="ghost" onClick={onCancel}>{t("cancel", lang)}</button>
      </div>
    </form>
  );
}
