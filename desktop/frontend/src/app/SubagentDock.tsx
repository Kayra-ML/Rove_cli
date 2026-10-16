import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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

// SubagentDock is the chat's subagents, as a strip over the message box: how
// many there are, then a card each — five in view, the rest a scroll away —
// with who is working on what, for how long, and what each one is doing right
// now. A card opens its details and buttons below the strip — after Hermes
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
  // the card whose details and buttons show under the strip
  const [picked, setPicked] = useState<string | null>(null);
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

  // Every subagent of the chat, on one strip: those at work first, then those
  // waiting for a slot (both in the order they were handed out), then those
  // whose work waits for a decision, then the finished ones, newest first.
  const ordered = useMemo(() => {
    const rank = (x: SubagentTask) => (x.status === "running" ? 0 : x.status === "queued" ? 1 : waiting(x) ? 2 : 3);
    return [...tasks].sort((a, b) => {
      const r = rank(a) - rank(b);
      if (r) return r;
      if (rank(a) <= 1) return a.batch === b.batch ? a.index - b.index : a.batch < b.batch ? -1 : 1;
      return (b.endedAt ?? "").localeCompare(a.endedAt ?? "");
    });
  }, [tasks]);

  const stripRef = useRef<HTMLDivElement | null>(null);
  const [edges, setEdges] = useState({ left: false, right: false });
  const measure = useCallback(() => {
    const el = stripRef.current;
    if (!el) return;
    const left = el.scrollLeft > 2;
    const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 2;
    setEdges((e) => (e.left === left && e.right === right ? e : { left, right }));
  }, []);
  useEffect(() => { measure(); }, [ordered.length, open, measure]);
  useEffect(() => {
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [measure]);
  // a page is the five cards in view
  const page = (dir: 1 | -1) => {
    const el = stripRef.current;
    if (el) el.scrollBy({ left: dir * el.clientWidth, behavior: "smooth" });
  };

  if (!session || ordered.length === 0) return null;

  const runningN = ordered.filter((x) => x.status === "running").length;
  const queuedN = ordered.filter((x) => x.status === "queued").length;
  const waitingN = ordered.filter((x) => done(x) && waiting(x)).length;
  const finishedN = ordered.length - runningN - queuedN - waitingN;
  const nameOf = (x: SubagentTask) => `${t("saOne", lang)} ${x.index}`;
  const sel = ordered.find((x) => x.id === picked) ?? null;

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
    <section className={`subagents strip${open ? " open" : ""}`} aria-label={t("subagents", lang)}>
      <div className="sa-bar">
        <button type="button" className="sa-head" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          {runningN > 0 ? <span className="sa-spin" aria-hidden /> : <Icon name="agents" size={13} />}
          <strong>{t("saAgents", lang)}</strong>
          <span className="sa-total" title={t("saTotalHint", lang)}>{ordered.length}</span>
          <span className="sa-counts">
            {[
              runningN && `${runningN} ${t("saRunning", lang)}`,
              queuedN && `${queuedN} ${t("saQueued", lang)}`,
              waitingN && `${waitingN} ${t("saWaitingShort", lang)}`,
              finishedN && `${finishedN} ${t("saDone", lang)}`,
            ].filter(Boolean).join(" · ")}
          </span>
          <span className="sa-caret" aria-hidden>›</span>
        </button>
        {open && ordered.length > 5 && (
          <span className="sa-pager">
            <button type="button" className="tree-icon" disabled={!edges.left} aria-label={t("saPrev", lang)} onClick={() => page(-1)}>‹</button>
            <button type="button" className="tree-icon" disabled={!edges.right} aria-label={t("saNext", lang)} onClick={() => page(1)}>›</button>
          </span>
        )}
      </div>
      {open && (
        <>
          <div
            className="sa-strip"
            ref={stripRef}
            onScroll={measure}
            // a mouse wheel scrolls the strip sideways
            onWheel={(e) => {
              const el = stripRef.current;
              if (!el || Math.abs(e.deltaX) > Math.abs(e.deltaY) || el.scrollWidth <= el.clientWidth) return;
              el.scrollLeft += e.deltaY;
            }}
          >
            {ordered.map((x) => {
              const line = x.status === "running"
                ? activity[x.sessionId] || t("thinking", lang)
                : x.error && x.status !== "interrupted" ? x.error : (x.summary ?? "").split("\n").find((l) => l.trim()) ?? "";
              return (
                <button
                  key={x.id}
                  type="button"
                  className={`sa-card st-${x.status}${picked === x.id ? " on" : ""}${waiting(x) && done(x) ? " wait" : ""}`}
                  onClick={() => { setPicked(picked === x.id ? null : x.id); setSteering(null); }}
                  onDoubleClick={() => openChannel(x)}
                  title={x.goal}
                >
                  <span className="sa-card-top">
                    <span className="sa-dot" title={t(LABEL[x.status], lang)} />
                    <strong>{nameOf(x)}</strong>
                    <span className="sa-time">{elapsed(x, now)}</span>
                  </span>
                  <span className="sa-card-goal">{x.goal}</span>
                  <span className={`sa-card-line${x.status === "failed" ? " bad" : ""}`}>{line || t(LABEL[x.status], lang)}</span>
                </button>
              );
            })}
          </div>
          {sel && (
            <div className={`sa-detail st-${sel.status}`}>
              <div className="sa-top">
                <span className="sa-dot" />
                <strong>{nameOf(sel)}</strong>
                <span className="sa-goal" title={sel.goal}>{sel.goal}</span>
                <span className="sa-acts">
                  {sel.status === "running" && (
                    <button type="button" className="ghost" onClick={() => { setSteering(steering === sel.id ? null : sel.id); setSteerText(""); }}>
                      {t("saSteer", lang)}
                    </button>
                  )}
                  {!done(sel) && (
                    <button type="button" className="ghost" disabled={busy === sel.id} onClick={() => void act(sel, "subagent.stop")}>{t("stop", lang)}</button>
                  )}
                  <button type="button" className="ghost" disabled={!sel.sessionId} onClick={() => openChannel(sel)}>{t("saOpen", lang)}</button>
                </span>
              </div>
              {(() => {
                const line = sel.status === "running"
                  ? activity[sel.sessionId] || t("thinking", lang)
                  : sel.error && sel.status !== "interrupted" ? sel.error : (sel.summary ?? "").trim();
                return line ? <div className={`sa-line${sel.status === "failed" ? " bad" : ""}`}>{line}</div> : null;
              })()}
              {sel.worktree && (sel.worktree.files?.length ?? 0) > 0 && (() => {
                const w = sel.worktree!;
                return (
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
                          <span className="sa-pending" title={w.pending}>
                            {t("saWaiting", lang)}{w.reason ? ` — ${t(`saWhy_${w.reason}`, lang)}` : ""}
                          </span>
                          {waiting(sel) && done(sel) && (
                            <span className="sa-acts">
                              <button type="button" className="ghost" disabled={busy === sel.id} onClick={() => void act(sel, "subagent.apply")}>{t("saApply", lang)}</button>
                              <button type="button" className="ghost danger-text" disabled={busy === sel.id} onClick={() => void act(sel, "subagent.discard")}>{t("saDiscard", lang)}</button>
                            </span>
                          )}
                        </>
                      )}
                  </div>
                );
              })()}
              {steering === sel.id && (
                <form
                  className="sa-steer"
                  onSubmit={(e) => {
                    e.preventDefault();
                    const text = steerText.trim();
                    if (!text) return;
                    void act(sel, "subagent.steer", { text }).then((ok) => {
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
          )}
        </>
      )}
    </section>
  );
}
