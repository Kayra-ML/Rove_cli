import { useEffect, useMemo, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { Session, SubagentStatus, SubagentTask } from "~/lib/types";
import { describeTool } from "~/lib/transcript";
import { t, type Lang } from "~/lib/i18n";
import { toast } from "~/lib/toast";
import { Icon } from "./Icons";

interface Props {
  // the chat the work was handed out from (its home channel)
  session: Session | null;
  lang: Lang;
  // open a subagent's own channel, to read what it did
  onOpen: (channel: Session) => void;
}

const LABEL: Record<SubagentStatus, string> = {
  queued: "saQueued",
  running: "saRunning",
  completed: "saDone",
  failed: "saFailed",
  interrupted: "saStopped",
  max_turns: "saCutShort",
};

const done = (x: SubagentTask) => x.status !== "queued" && x.status !== "running";
// A subagent in its own copy works under a long temporary folder; its tool
// calls name files by that path, which says nothing to the user. The tail of
// the path is the part that means something.
const short = (arg: string) => {
  if (!arg.startsWith("/")) return arg;
  const parts = arg.split("/").filter(Boolean);
  return parts.length > 2 ? "…/" + parts.slice(-2).join("/") : arg;
};
// a checkout whose work did not land and is still on disk: someone has to
// decide what becomes of it
const waiting = (x: SubagentTask) => Boolean(x.worktree && !x.worktree.applied && x.worktree.path);

function elapsed(x: SubagentTask, now: number): string {
  if (!x.startedAt) return "";
  const from = Date.parse(x.startedAt);
  const to = x.endedAt ? Date.parse(x.endedAt) : now;
  const s = Math.max(0, Math.round((to - from) / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

// SubagentDock is the chat's subagents, over the message box: who is working
// on what, for how long, and what each one is doing right now — after Hermes
// Agent's roster — and, after Orca, where each one's work ended up: in the
// project, or waiting in its own copy because it clashed with another's.
// Each can be stopped, steered while it runs, and opened to read in full.
export function SubagentDock({ session, lang, onOpen }: Props) {
  const chatId = session ? (session.parentId || session.id) : "";
  const [tasks, setTasks] = useState<SubagentTask[]>([]);
  // what each running subagent is doing, from its latest tool call
  const [activity, setActivity] = useState<Record<string, string>>({});
  const [open, setOpen] = useState(true);
  const [steering, setSteering] = useState<string | null>(null);
  const [steerText, setSteerText] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    setTasks([]);
    setActivity({});
    if (!chatId) return;
    let live = true;
    rpc<SubagentTask[]>("subagent.list", { sessionId: chatId })
      .then((list) => { if (live) setTasks(list ?? []); })
      .catch(() => { if (live) setTasks([]); });
    return () => { live = false; };
  }, [chatId]);

  useEffect(() => {
    if (!chatId) return;
    const offs = [
      subscribeEvents("subagent.updated", (ev) => {
        const p = (ev.payload ?? {}) as { sessionId?: string; tasks?: SubagentTask[] };
        if (p.sessionId !== chatId) return;
        setTasks((old) => {
          const next = p.tasks ?? [];
          // a new call opens the dock again, even if it was folded
          if (next.length && next[next.length - 1].batch !== old[old.length - 1]?.batch) setOpen(true);
          return next;
        });
      }),
      subscribeEvents("tool.start", (ev) => {
        const p = (ev.payload ?? {}) as { name?: string; args?: unknown; sessionId?: string };
        if (!p.sessionId || !p.name) return;
        const tv = describeTool("", p.name, typeof p.args === "string" ? p.args : JSON.stringify(p.args ?? {}));
        const line = [tv.verb, short(tv.arg)].filter(Boolean).join(" ");
        setActivity((a) => (a[p.sessionId!] === line ? a : { ...a, [p.sessionId!]: line }));
      }),
    ];
    return () => offs.forEach((f) => f());
  }, [chatId]);

  const running = tasks.some((x) => !done(x));
  useEffect(() => {
    if (!running) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [running]);

  // the latest call, plus anything older still working or still waiting
  const shown = useMemo(() => {
    const last = tasks.length ? tasks[tasks.length - 1].batch : "";
    return tasks.filter((x) => x.batch === last || !done(x) || waiting(x));
  }, [tasks]);

  if (!session || shown.length === 0) return null;

  const counts = new Map<string, number>();
  for (const x of shown) counts.set(LABEL[x.status], (counts.get(LABEL[x.status]) ?? 0) + 1);
  const nameOf = (x: SubagentTask) => `${t("saOne", lang)} ${x.index}`;

  const act = async (x: SubagentTask, method: string, extra: Record<string, unknown> = {}) => {
    setBusy(x.id);
    try {
      await rpc(method, { id: x.id, ...extra });
      return true;
    } catch (e) {
      // applying fails for one reason worth saying plainly; git's words stay
      // on the row's tooltip
      toast(method === "subagent.apply" ? t("saStillClashes", lang) : e instanceof Error ? e.message : method, "err");
      return false;
    } finally {
      setBusy(null);
    }
  };

  const openChannel = (x: SubagentTask) => {
    if (!x.sessionId) return;
    onOpen({
      id: x.sessionId, title: nameOf(x), agentId: session.agentId, workspaceId: session.workspaceId,
      parentId: chatId, updatedAt: session.updatedAt, space: "worker",
    });
  };

  return (
    <section className={`subagents${open ? " open" : ""}`} aria-label={t("subagents", lang)}>
      <button type="button" className="sa-head" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {running ? <span className="sa-spin" aria-hidden /> : <Icon name="agents" size={13} />}
        <strong>{t("subagents", lang)}</strong>
        <span className="sa-counts">
          {[...counts].map(([k, n]) => `${n} ${t(k, lang)}`).join(" · ")}
        </span>
        <span className="sa-caret" aria-hidden>›</span>
      </button>
      {open && (
        <ul className="sa-list">
          {shown.map((x) => {
            const w = x.worktree;
            const line = x.status === "running"
              ? activity[x.sessionId] || t("thinking", lang)
              : x.error && x.status !== "interrupted" ? x.error : (x.summary ?? "").split("\n").find((l) => l.trim()) ?? "";
            return (
              <li key={x.id} className={`sa-row st-${x.status}`}>
                <span className="sa-dot" title={t(LABEL[x.status], lang)} />
                <div className="sa-main">
                  <div className="sa-top">
                    <button type="button" className="sa-name" title={t("saOpen", lang)} onClick={() => openChannel(x)} disabled={!x.sessionId}>
                      <strong>{nameOf(x)}</strong>
                    </button>
                    <span className="sa-goal" title={x.goal}>{x.goal}</span>
                    <span className="sa-time">{elapsed(x, now)}</span>
                    {!done(x) && (
                      <span className="sa-acts">
                        {x.status === "running" && (
                          <button type="button" className="ghost" onClick={() => { setSteering(steering === x.id ? null : x.id); setSteerText(""); }}>
                            {t("saSteer", lang)}
                          </button>
                        )}
                        <button type="button" className="ghost" disabled={busy === x.id} onClick={() => void act(x, "subagent.stop")}>
                          {t("stop", lang)}
                        </button>
                      </span>
                    )}
                  </div>
                  {line && <div className={`sa-line${x.status === "failed" ? " bad" : ""}`} title={line}>{line}</div>}
                  {w && (w.files?.length ?? 0) > 0 && (
                    <div className="sa-work">
                      <span className="sa-diff">
                        <span className="edit-add">+{w.added}</span> <span className="edit-del">−{w.removed}</span>
                        {" · "}{w.files!.length} {t("saFiles", lang)}
                      </span>
                      {w.applied
                        ? <span className="sa-landed">{t("saApplied", lang)}</span>
                        : w.reason === "discarded"
                        ? <span className="sa-gone">{t("saDiscarded", lang)}</span>
                        : (
                          <>
                            {/* in the user's words; git's own message stays in the tooltip */}
                            <span className="sa-pending" title={w.pending}>
                              {t("saWaiting", lang)}{w.reason ? ` — ${t(`saWhy_${w.reason}`, lang)}` : ""}
                            </span>
                            {waiting(x) && done(x) && (
                              <span className="sa-acts">
                                <button type="button" className="ghost" disabled={busy === x.id} onClick={() => void act(x, "subagent.apply")}>{t("saApply", lang)}</button>
                                <button type="button" className="ghost danger-text" disabled={busy === x.id} onClick={() => void act(x, "subagent.discard")}>{t("saDiscard", lang)}</button>
                              </span>
                            )}
                          </>
                        )}
                    </div>
                  )}
                  {steering === x.id && (
                    <form
                      className="sa-steer"
                      onSubmit={(e) => {
                        e.preventDefault();
                        const text = steerText.trim();
                        if (!text) return;
                        void act(x, "subagent.steer", { text }).then((ok) => {
                          if (!ok) return;
                          setSteering(null);
                          setSteerText("");
                          toast(t("saSteerSent", lang));
                        });
                      }}
                    >
                      <input autoFocus value={steerText} placeholder={t("saSteerHint", lang)} onChange={(e) => setSteerText(e.target.value)}
                        onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); setSteering(null); } }} />
                    </form>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
