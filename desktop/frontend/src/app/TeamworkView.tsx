import { Fragment, useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { rpc } from "~/lib/rpc";
import type { Session, WorkNote, WorkPhase, WorkPlan, Workspace } from "~/lib/types";
import { useTeamwork } from "~/hooks/useTeamwork";
import { useCharacterName } from "~/hooks/usePersona";
import { usePrefs } from "~/hooks/usePrefs";
import { CARD_W, cableState, graphOf, statsOf } from "~/lib/teamwork";
import type { ModelChoice } from "~/lib/slash";
import { t, type Lang } from "~/lib/i18n";
import { toast } from "~/lib/toast";
import { AgentMark } from "./AgentMark";
import { Chat } from "./Chat";
import { Icon } from "./Icons";

interface Props {
  session: Session | null;
  // a new chat not created yet: its first plan creates it
  draft: boolean;
  onCreateSession: () => Promise<Session>;
  workspace: Workspace | null;
}

// TeamworkView is the Teamwork mode of a chat: one request, split into
// parts that orchestras take on — a conductor and its members each — wired
// by cables where one part needs another's work, meeting at junctions and
// ending in a merge orchestra. The orchestras stand on the left, column by
// column; the panel on the right is the conversation: the request, the plan
// and its box, or — once a card or junction is opened — that orchestra's
// chats, with a lit cable running from what is open to the panel. Nothing
// runs until the user approves the plan.
export function TeamworkView({ session, draft, onCreateSession, workspace }: Props) {
  const { lang } = usePrefs();
  const { plan, setPlan, loaded } = useTeamwork(draft ? null : session?.id);
  const [prompt, setPrompt] = useState("");
  const [planning, setPlanning] = useState(false);
  const [open, setOpen] = useState<Sel | null>(null);
  const [busy, setBusy] = useState(false);
  // the planner's model: null follows the last plan's (or the chat's);
  // an empty choice is the chat's model on purpose
  const [picked, setPicked] = useState<ModelChoice | null>(null);
  const [models, setModels] = useState<ModelChoice[] | null>(null);
  const [chatModel, setChatModel] = useState<ModelChoice | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const pillRef = useRef<HTMLButtonElement | null>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const stageRef = useRef<HTMLElement | null>(null);
  const panelRef = useRef<HTMLElement | null>(null);

  useEffect(() => { setOpen(null); setPicked(null); setModels(null); }, [session?.id]);
  useEffect(() => {
    if (!session) { setChatModel(null); return; }
    let live = true;
    rpc<ModelChoice>("session.model", { sessionId: session.id, agentId: session.agentId })
      .then((m) => { if (live) setChatModel(m?.model ? m : null); })
      .catch(() => { if (live) setChatModel(null); });
    return () => { live = false; };
  }, [session?.id, session?.agentId]);
  // the model menu closes on a click outside it or on Escape
  useEffect(() => {
    if (!models) return;
    const away = (e: MouseEvent) => {
      const el = e.target as Node;
      if (!menuRef.current?.contains(el) && !pillRef.current?.contains(el)) setModels(null);
    };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") setModels(null); };
    document.addEventListener("mousedown", away);
    document.addEventListener("keydown", esc);
    return () => { document.removeEventListener("mousedown", away); document.removeEventListener("keydown", esc); };
  }, [models]);

  const planner: ModelChoice | null = picked ? (picked.model ? picked : null) : (plan?.planner?.model ? plan.planner : null);
  const plannerLabel = planner?.model || chatModel?.model || t("twPlannerChat", lang);
  const sameModel = (a: ModelChoice | null, b: ModelChoice) => Boolean(a && a.model === b.model && (!a.provider || !b.provider || a.provider === b.provider));

  const openModels = async () => {
    const list = (await rpc<ModelChoice[]>("model.list").catch(() => [])) ?? [];
    setModels(list);
  };

  const running = plan?.status === "running";
  const isDraft = plan?.status === "draft";
  const stopped = plan?.status === "failed" || plan?.status === "canceled" || plan?.status === "interrupted";

  // makePlan plans the request in the box, or re-plans the current one
  const makePlan = async (again?: string) => {
    const text = (again ?? prompt).trim();
    if (!text || planning || running) return;
    setPlanning(true);
    setModels(null);
    try {
      const s = session ?? (await onCreateSession());
      const model = planner ? { provider: planner.provider, model: planner.model } : {};
      const p = await rpc<WorkPlan>("teamwork.plan", { sessionId: s.id, prompt: text, ...model });
      setPlan(p);
      setOpen(null);
      if (again === undefined) setPrompt("");
      window.dispatchEvent(new Event("rove:sessions"));
    } catch (e) {
      toast(e instanceof Error ? e.message : "plan failed", "err");
    } finally {
      setPlanning(false);
    }
  };

  const act = useCallback(async (method: string, params: Record<string, unknown>) => {
    setBusy(true);
    try {
      const p = await rpc<WorkPlan | { ok: boolean }>(method, params);
      if (p && "phases" in p) setPlan(p);
      else setPlan(null);
    } catch (e) {
      toast(e instanceof Error ? e.message : method, "err");
    } finally {
      setBusy(false);
    }
  }, [setPlan]);

  // Escape steps back from an open orchestra to the plan
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape" && !e.defaultPrevented) setOpen(null); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  const wire = useWire(rootRef, stageRef, panelRef, open, plan);

  if (!draft && session && !loaded) return <div className="tw tw-loading" />;

  const stats = plan ? statsOf(plan) : null;
  const result = plan?.status === "done" ? finalReport(plan) : undefined;
  const showing = open && session && plan ? open : null;

  return (
    <div className="tw" ref={rootRef}>
      <section className="tw-stage" ref={stageRef} aria-label={t("twOrchestraChats", lang)}>
        {!plan ? (
          <div className="tw-stage-empty">
            <Icon name="teamwork" size={28} />
            <p>{t("twAskHint", lang)}</p>
          </div>
        ) : (
          <Board
            plan={plan}
            lang={lang}
            sel={showing}
            editable={isDraft && !busy}
            onOpen={setOpen}
            onRemovePhase={(id) => void act("teamwork.removePhase", { planId: plan.id, phase: id })}
            onRemoveAgent={(id, c) => void act("teamwork.removeAgent", { planId: plan.id, phase: id, character: c })}
          />
        )}
      </section>

      <aside className="tw-panel" ref={panelRef} aria-label={plan?.summary || t("twAskTitle", lang)}>
        {showing && session && plan ? (
          <OrchestraPanel key={`${showing.kind}:${showing.ids.join(",")}`} sel={showing} plan={plan} session={session} workspace={workspace} lang={lang} onClose={() => setOpen(null)} />
        ) : (
          <>
            <header className="tw-panel-head">
              <div className="tw-panel-title">
                <strong title={plan?.prompt}>{plan ? plan.summary || plan.prompt : t("twAskTitle", lang)}</strong>
                <span>{wsName(workspace) ?? t("twHere", lang)} · Teamwork</span>
              </div>
              {plan && <span className={`tw-status ${plan.status}`}>{t(`twStatus_${plan.status}`, lang)}</span>}
            </header>
            {/* the planner's model sits up here, so the request box below
                keeps its whole width for the request */}
            <div className="tw-planner-bar">
              <span className="tw-planner-label">{t("twPlanner", lang)}</span>
              <div className="tw-planner-wrap">
                <button
                  type="button"
                  ref={pillRef}
                  className={`model-pill tw-planner${models ? " open" : ""}`}
                  aria-label={t("twPlanner", lang)}
                  aria-haspopup="listbox"
                  aria-expanded={Boolean(models)}
                  title={`${t("twPlanner", lang)}: ${plannerLabel}`}
                  disabled={planning || running}
                  onClick={() => { if (models) setModels(null); else void openModels(); }}
                >
                  <Icon name="lightning" size={12} />
                  <span className="pill-label">{plannerLabel}</span>
                  <span className="profile-caret">⌄</span>
                </button>
                {models && (
                  <div className="slash-menu model-menu from-pill tw-planner-menu" role="listbox" aria-label={t("twPlanner", lang)} ref={menuRef}>
                    <div className="slash-head">
                      <span>{t("twPlanner", lang)}</span>
                      <span className="term-dim">{plannerLabel}</span>
                    </div>
                    {[{ provider: "", model: "" }, ...models].map((m, i, rows) => (
                      <Fragment key={`${m.provider}/${m.model}`}>
                        {m.model && m.provider !== rows[i - 1]?.provider && <div className="model-group">{m.provider}</div>}
                        <button
                          type="button"
                          role="option"
                          aria-selected={m.model ? sameModel(planner, m) : !planner}
                          className={`slash-row${(m.model ? sameModel(planner, m) : !planner) ? " current" : ""}`}
                          onClick={() => { setPicked(m); setModels(null); }}
                        >
                          {m.model ? (
                            <>
                              <code>{m.model}</code>
                              {sameModel(planner, m) && <span className="slash-check">✓</span>}
                            </>
                          ) : (
                            <>
                              <code className="slash-reset">↺ {t("twPlannerChat", lang)}</code>
                              {chatModel?.model && <span>{chatModel.model}</span>}
                              {!planner && <span className="slash-check">✓</span>}
                            </>
                          )}
                        </button>
                      </Fragment>
                    ))}
                  </div>
                )}
              </div>
            </div>
            <div className="tw-thread">
              {!plan || !stats ? (
                <div className="tw-empty">
                  <strong>{t("twAskTitle", lang)}</strong>
                  <p>{t("twAskHint", lang)}</p>
                </div>
              ) : (
                <>
                  <div className="tw-bubble">{plan.prompt}</div>
                  <div className="tw-reply">
                    <strong className="tw-count">{stats.phases} {t("twPhases", lang)} · {stats.agents} {t("twAgents", lang)}</strong>
                    {plan.summary && plan.summary !== plan.prompt && <p>{plan.summary}</p>}
                    <div className="tw-chips">
                      {stats.parallel > 0 && <span className="tw-chip"><Icon name="branch" size={12} /> {stats.parallel} {t("twParallel", lang)} → {t("twMerge", lang)}</span>}
                      {stats.junctions > 0 && <span className="tw-chip"><Icon name="merge" size={12} /> {stats.junctions} {t("twJunction", lang).toLowerCase()}</span>}
                      {plan.planner?.model && (
                        <span className="tw-chip" title={`${t("twPlannedBy", lang)}: ${plan.planner.provider ? `${plan.planner.provider}/` : ""}${plan.planner.model}`}>
                          <Icon name="lightning" size={12} /> {plan.planner.model}
                        </span>
                      )}
                    </div>
                  </div>
                  {(plan.notes ?? []).map((n, i) => (
                    <div key={i} className="tw-rule"><span className="tw-note">{noteText(n, lang)}</span></div>
                  ))}
                  {plan.verify && (
                    <section className={`tw-verify ${plan.verify.verified ? "ok" : "bad"}`}>
                      <div className="tw-verify-head">
                        <span className={`tw-dot ${plan.verify.verified ? "done" : "failed"}`} />
                        <strong>{t(plan.verify.verified ? "twVerified" : "twNotVerified", lang)}</strong>
                        {plan.verify.rounds > 1 && <span className="tw-chip">{t("twFixedOnce", lang)}</span>}
                      </div>
                      {(plan.verify.checks ?? []).length > 0 && (
                        <ul className="tw-checks">{plan.verify.checks!.map((c, i) => <li key={i}><code>{c}</code></li>)}</ul>
                      )}
                      {(plan.verify.problems ?? []).length > 0 && (
                        <ul className="tw-problems">{plan.verify.problems!.map((c, i) => <li key={i}>{c}</li>)}</ul>
                      )}
                    </section>
                  )}
                  {result && (
                    <section className="tw-result">
                      <div className="section-label">{t("twResult", lang)}</div>
                      <pre>{result}</pre>
                    </section>
                  )}
                </>
              )}
            </div>

            <div className={`composer-dock tw-dock${prompt.trim() ? " filled" : ""}`}>
              {planning && (
                <div className="status-stack"><span className="pip run" /><span>{t("twPlanning", lang)}</span></div>
              )}
              {!planning && plan && isDraft && (
                <div className="status-stack tw-bar">
                  <span className="tw-bar-text">{t("twApproveHint", lang)}</span>
                  <div className="tw-bar-actions">
                    <button type="button" className="ghost" disabled={busy} onClick={() => void act("teamwork.discard", { planId: plan.id })}>{t("twDiscard", lang)}</button>
                    <button type="button" className="ghost" disabled={busy} title={`${t("twPlanner", lang)}: ${plannerLabel}`} onClick={() => void makePlan(plan.prompt)}>{t("twReplan", lang)}</button>
                    <button type="button" className="primary" disabled={busy} onClick={() => void act("teamwork.approve", { planId: plan.id })}>{t("twApprove", lang)}</button>
                  </div>
                </div>
              )}
              {!planning && plan && stopped && (
                <div className="status-stack tw-bar">
                  <span className={`tw-dot ${plan.status}`} />
                  <span>{t(`twStatus_${plan.status}`, lang)} · {plan.phases.filter((p) => p.status === "done").length}/{plan.phases.length} {t("twDoneKept", lang)}</span>
                  <span className="tw-spacer" />
                  <button type="button" className="primary" disabled={busy} onClick={() => void act("teamwork.retry", { planId: plan.id })}>{t("twRetry", lang)}</button>
                </div>
              )}
              {!planning && plan && running && (
                <div className="status-stack tw-bar">
                  <span className="pip run" />
                  <span>{plan.verifying ? t("twVerifying", lang) : `${t("twStatus_running", lang)} · ${plan.phases.filter((p) => p.status === "done").length}/${plan.phases.length}`}</span>
                  <span className="tw-spacer" />
                  <button type="button" className="ghost" disabled={busy} onClick={() => void act("teamwork.cancel", { planId: plan.id })}>{t("stop", lang)}</button>
                </div>
              )}
              <form className="composer-root" onSubmit={(e) => { e.preventDefault(); void makePlan(); }}>
                <div className="composer-surface">
                  <div className="composer-fade">
                    <div className="composer-row">
                      <div className="composer-menu tw-dock-icon"><Icon name="teamwork" size={15} /></div>
                      <textarea
                        className="composer-input"
                        rows={1}
                        value={prompt}
                        placeholder={t("twAskPlaceholder", lang)}
                        onChange={(e) => setPrompt(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void makePlan(); }
                        }}
                        disabled={planning || running}
                      />
                      <div className="composer-controls">
                        <button type="submit" className="send-btn" aria-label={t("twMakePlan", lang)} title={t("twMakePlan", lang)} disabled={!prompt.trim() || planning || running}>↑</button>
                      </div>
                    </div>
                  </div>
                </div>
              </form>
            </div>
          </>
        )}
      </aside>

      {wire && (
        <svg className="tw-wire" aria-hidden="true">
          <path d={wire.d} className="tw-wire-glow" />
          <path d={wire.d} className={`tw-wire-line${wire.running ? " running" : ""}`} />
          <circle cx={wire.x2} cy={wire.y2} r={4.5} className="tw-wire-port" />
        </svg>
      )}
    </div>
  );
}

// What the panel shows: one orchestra, or the orchestras meeting at a
// junction (the parts whose cables meet there, and the part they feed).
type Sel = { kind: "orchestra" | "junction"; ids: string[]; focus: string };

type Wire = { d: string; x2: number; y2: number; running: boolean };

// useWire draws the cable from the open card (or junction) across to the
// panel, where its chats are: the one lit cable on the page, the thread
// being read. It follows the board as it scrolls and the window as it
// resizes, and is dropped while its end is scrolled out of sight.
function useWire(
  rootRef: RefObject<HTMLDivElement | null>,
  stageRef: RefObject<HTMLElement | null>,
  panelRef: RefObject<HTMLElement | null>,
  sel: Sel | null,
  plan: WorkPlan | null,
): Wire | null {
  const [wire, setWire] = useState<Wire | null>(null);
  const measure = useCallback(() => {
    const root = rootRef.current, stage = stageRef.current, panel = panelRef.current;
    if (!root || !stage || !panel || !sel || !plan) { setWire(null); return; }
    const at = sel.kind === "junction" ? `[data-junction="j-${sel.focus}"]` : `[data-phase="${sel.focus}"]`;
    const el = stage.querySelector<HTMLElement>(at);
    if (!el) { setWire(null); return; }
    const o = root.getBoundingClientRect(), s = stage.getBoundingClientRect(), pn = panel.getBoundingClientRect(), r = el.getBoundingClientRect();
    const x1 = (sel.kind === "junction" ? r.left + r.width / 2 : r.right) - o.left;
    const y1 = r.top + r.height / 2 - o.top;
    const x2 = pn.left - o.left;
    // the panel takes the cable at the card's height, kept off its corners
    const y2 = Math.min(Math.max(y1, pn.top - o.top + 64), pn.bottom - o.top - 64);
    const seen = r.top + r.height / 2 >= s.top && r.top + r.height / 2 <= s.bottom && x1 <= s.right - o.left + 1;
    if (!seen || x2 - x1 < 12) { setWire(null); return; }
    const dx = Math.max(24, (x2 - x1) * 0.5);
    const status = plan.phases.find((p) => p.id === sel.focus)?.status;
    setWire({ d: `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`, x2, y2, running: status === "running" });
  }, [rootRef, stageRef, panelRef, sel, plan]);

  useLayoutEffect(() => {
    measure();
    const stage = stageRef.current;
    stage?.addEventListener("scroll", measure, { passive: true });
    window.addEventListener("resize", measure);
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    if (rootRef.current) ro?.observe(rootRef.current);
    return () => {
      stage?.removeEventListener("scroll", measure);
      window.removeEventListener("resize", measure);
      ro?.disconnect();
    };
  }, [measure, rootRef, stageRef]);
  return wire;
}

function Board({ plan, lang, sel, editable, onOpen, onRemovePhase, onRemoveAgent }: {
  plan: WorkPlan;
  lang: Lang;
  sel: Sel | null;
  editable: boolean;
  onOpen: (sel: Sel) => void;
  onRemovePhase: (id: string) => void;
  onRemoveAgent: (id: string, character: string) => void;
}) {
  const g = graphOf(plan);
  const work = plan.phases.filter((p) => !p.merge).length;
  const clickable = plan.status !== "draft";
  const byId = new Map(plan.phases.map((p) => [p.id, p]));
  const junctionOf = new Map(g.junctions.map((j) => [j.id, j]));
  const lit = new Set(sel?.ids ?? []);
  return (
    <div className="tw-board">
      <div className="tw-graph" style={{ width: g.width, height: g.height }}>
        {g.columns.map((c) => (
          <div key={c.x} className="tw-col-label" style={{ left: c.x, top: c.y, width: CARD_W }}>
            {c.merge ? t("twMerge", lang) : `${t("twWaveN", lang).replace("{n}", String(c.wave))}${c.wave === 1 ? ` · ${t("twStartsNow", lang)}` : ""}`}
          </div>
        ))}
        <svg className="tw-links" width={g.width} height={g.height} aria-hidden="true">
          {g.cables.map((c) => (
            <path key={c.key} d={c.d} className={`tw-cable ${cableState(plan, c, junctionOf.get(c.from))}${lit.has(c.from) && lit.has(c.to) ? " lit" : ""}`} />
          ))}
        </svg>
        {g.junctions.map((j) => {
          const into = byId.get(j.to);
          const state = cableState(plan, { key: j.id, from: j.id, to: j.to, d: "", into: "out" }, j);
          const on = sel?.kind === "junction" && sel.focus === j.to;
          return (
            <button
              key={j.id}
              type="button"
              data-junction={j.id}
              className={`tw-junction ${state}${on ? " open" : ""}`}
              style={{ left: j.x, top: j.y }}
              disabled={!clickable}
              title={`${t("twJunction", lang)}: ${j.from.join(" + ")} → ${into?.merge ? t("twMerge", lang) : j.to}`}
              onClick={() => onOpen({ kind: "junction", ids: [...j.from, j.to], focus: j.to })}
            >
              <span className="tw-junction-n">{j.from.length}</span>
            </button>
          );
        })}
        {plan.phases.map((p) => {
          const r = g.nodes.get(p.id);
          if (!r) return null;
          return (
            <OrchestraCard
              key={p.id}
              phase={p}
              lang={lang}
              style={{ left: r.x, top: r.y, width: r.w, height: r.h }}
              open={sel?.kind === "orchestra" && sel.focus === p.id}
              meets={sel?.kind === "junction" && lit.has(p.id)}
              clickable={clickable}
              removable={editable && !p.merge && work > 1}
              editable={editable}
              onOpen={() => onOpen({ kind: "orchestra", ids: [p.id], focus: p.id })}
              onRemove={() => onRemovePhase(p.id)}
              onRemoveAgent={(c) => onRemoveAgent(p.id, c)}
            />
          );
        })}
      </div>
    </div>
  );
}

// OrchestraCard is one part: who conducts it, who plays in it, when it
// starts and how it is doing — with a port on each side for its cables.
function OrchestraCard({ phase: p, lang, style, open, meets, clickable, removable, editable, onOpen, onRemove, onRemoveAgent }: {
  phase: WorkPhase;
  lang: Lang;
  style: CSSProperties;
  open: boolean;
  meets: boolean;
  clickable: boolean;
  removable: boolean;
  editable: boolean;
  onOpen: () => void;
  onRemove: () => void;
  onRemoveAgent: (character: string) => void;
}) {
  const charName = useCharacterName();
  const status = p.status ?? "draft";
  const [lead, ...members] = p.agents;
  const deps = p.dependsOn ?? [];
  const title = p.merge ? t("twMergeTitle", lang) : p.title;
  const when = p.merge ? t("twAllBranches", lang) : deps.length ? t("twStartsWhen", lang).replace("{ids}", deps.join(", ")) : t("twStartsNow", lang);
  return (
    <article
      className={`tw-card ${status}${p.merge ? " merge" : ""}${clickable ? " clickable" : ""}${open ? " open" : ""}${meets ? " meets" : ""}`}
      style={style}
      data-phase={p.id}
      onClick={clickable ? onOpen : undefined}
    >
      <span className="tw-port in" />
      <span className="tw-port out" />
      <div className="tw-card-head">
        <span className="tw-card-mark">
          {p.merge || !lead ? <Icon name="merge" size={15} /> : <AgentMark seed={lead.character} character={lead.character} size={20} />}
        </span>
        <div className="tw-card-name">
          <div className="tw-card-title-row">
            <span className="tw-card-title" title={title}>{title}</span>
            <span className="tw-id">{p.merge ? <Icon name="merge" size={11} /> : p.id}</span>
          </div>
          <span className="tw-when" title={when}>{when}</span>
        </div>
        {removable && (
          <button type="button" className="tree-icon tw-x" title={t("twRemovePhase", lang)} onClick={(e) => { e.stopPropagation(); onRemove(); }}>×</button>
        )}
      </div>
      <div className="tw-band">
        {lead && (
          <span className="tw-player lead" title={t("twConductor", lang)}>
            <span className="tw-role">{t("twConductor", lang)}</span>
            <span className="tw-pname">{charName(lead.character, lead.character)}</span>
          </span>
        )}
        {members.length === 0 && <span className="tw-player solo"><span className="tw-pname">{t("twAlone", lang)}</span></span>}
        {members.map((a) => (
          <span key={a.character} className="tw-player" title={a.why}>
            <span className="tw-pname">{charName(a.character, a.character)}</span>
            {editable && (
              <button type="button" className="tw-helper-x" title={t("twRemoveAgent", lang)} onClick={(e) => { e.stopPropagation(); onRemoveAgent(a.character); }}>×</button>
            )}
          </span>
        ))}
      </div>
      <div className={`tw-card-foot ${status}`}>
        <span className={`tw-dot ${status}`} />
        {p.error ? <span className="tw-error" title={p.error}>{p.error}</span> : <span>{t(`twStatus_${status}`, lang)}</span>}
      </div>
    </article>
  );
}

// OrchestraPanel takes the panel's place while an orchestra is open: the
// orchestras of a card or of a junction, and for the one picked, each
// player's own chat — the conductor's, where the part is led and handed
// out, and each member's — chosen from the pill in the corner.
function OrchestraPanel({ sel, plan, session, workspace, lang, onClose }: {
  sel: Sel;
  plan: WorkPlan;
  session: Session;
  workspace: Workspace | null;
  lang: Lang;
  onClose: () => void;
}) {
  const charName = useCharacterName();
  const list = sel.ids.map((id) => plan.phases.find((p) => p.id === id)).filter((p): p is WorkPhase => Boolean(p));
  const [pick, setPick] = useState(sel.focus);
  const [who, setWho] = useState(0);
  const [menu, setMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => setWho(0), [pick]);
  useEffect(() => {
    if (!menu) return;
    const away = (e: MouseEvent) => { if (!menuRef.current?.contains(e.target as Node)) setMenu(false); };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") { e.preventDefault(); setMenu(false); } };
    document.addEventListener("mousedown", away);
    window.addEventListener("keydown", esc, true);
    return () => { document.removeEventListener("mousedown", away); window.removeEventListener("keydown", esc, true); };
  }, [menu]);
  const phase = list.find((p) => p.id === pick) ?? list[0];
  if (!phase) return null;
  const at = Math.min(who, phase.agents.length - 1);
  const a = phase.agents[at];
  const lead = phase.agents[0]?.channelId;
  const channel: Session | null = a?.channelId
    ? { id: a.channelId, title: phase.title, agentId: session.agentId, workspaceId: session.workspaceId, parentId: at === 0 ? session.id : lead, updatedAt: session.updatedAt }
    : null;
  const status = phase.status ?? plan.status;
  const junction = sel.kind === "junction";
  const name = (p: WorkPhase) => (p.merge ? t("twMerge", lang) : p.title);
  return (
    <section className="tw-orch" role="region" aria-label={junction ? t("twJunction", lang) : name(phase)}>
      <header className="tw-panel-head">
        <button type="button" className="tree-icon tw-back" title={t("twBackToPlan", lang)} aria-label={t("twBackToPlan", lang)} onClick={onClose}>←</button>
        <div className="tw-panel-title">
          <strong title={phase.merge ? t("twMergeTitle", lang) : phase.title}>{junction ? `${t("twJunction", lang)} → ${name(list[list.length - 1])}` : name(phase)}</strong>
          <span>{phase.merge ? t("twMerge", lang) : phase.id} · {t(`twStatus_${status}`, lang)}</span>
        </div>
        {a && (
          <div className="tw-who-wrap" ref={menuRef}>
            <button
              type="button"
              className={`tw-who-pill${menu ? " open" : ""}`}
              aria-haspopup="listbox"
              aria-expanded={menu}
              aria-label={t("twPlayer", lang)}
              onClick={() => setMenu((m) => !m)}
            >
              <span className={`tw-dot ${status}`} />
              <AgentMark seed={a.character} character={a.character} size={14} />
              <span className="tw-who-name">{charName(a.character, a.character)}</span>
              <span className="profile-caret">⌄</span>
            </button>
            {menu && (
              <div className="tw-who" role="listbox" aria-label={t("twOrchestraChats", lang)}>
                {phase.agents.map((x, i) => (
                  <button
                    key={x.character}
                    type="button"
                    role="option"
                    aria-selected={i === at}
                    className={i === at ? "on" : ""}
                    title={x.why}
                    onClick={() => { setWho(i); setMenu(false); }}
                  >
                    <AgentMark seed={x.character} character={x.character} size={16} />
                    <span className="tw-who-name">{charName(x.character, x.character)}</span>
                    {i === 0 ? <span className="tw-role">{t("twConductor", lang)}</span> : x.why && <span className="tw-why">{x.why}</span>}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </header>
      {junction && (
        <nav className="tw-rail" aria-label={t("twJunctionHint", lang)}>
          {list.map((p, i) => (
            <button key={p.id} type="button" className={`tw-rail-row${p.id === phase.id ? " on" : ""}`} title={name(p)} onClick={() => setPick(p.id)}>
              <span className={`tw-dot ${p.status ?? "draft"}`} />
              <span className="tw-id">{p.merge ? <Icon name="merge" size={11} /> : p.id}</span>
              <span className="tw-rail-name">{name(p)}</span>
              {i === list.length - 1 && <span className="tw-rail-into">{t("twJunctionInto", lang)}</span>}
            </button>
          ))}
        </nav>
      )}
      {at === 0 && phase.goal && <p className="tw-goal">{phase.goal}</p>}
      {at > 0 && a?.why && <p className="tw-goal">{a.why}</p>}
      {phase.report && at === 0 && (
        <details className="tw-report-box">
          <summary>{t("twResult", lang)}</summary>
          <pre className="tw-report">{phase.report}</pre>
        </details>
      )}
      <div className="tw-panel-chat">
        {channel ? (
          <Chat key={channel.id} session={channel} workspaceId={workspace?.id} workspacePath={workspace?.path} />
        ) : (
          <p className="map-muted tw-not-started">{t("twNotStarted", lang)}</p>
        )}
      </div>
    </section>
  );
}

// wsName is the folder's name as the user knows it: a workspace opened at
// the disk's root is named "/", which says nothing.
function wsName(w: Workspace | null): string | undefined {
  const n = w?.name?.trim();
  return n && n !== "/" ? n : undefined;
}

// finalReport is what the user reads when a plan is done: the merge's
// report, or the only phase's when the job was one phase.
function finalReport(plan: WorkPlan): string | undefined {
  const merge = plan.phases.find((p) => p.merge);
  if (merge) return merge.report;
  return plan.phases.length === 1 ? plan.phases[0].report : undefined;
}

// noteText explains, in the user's language, what the validator changed.
function noteText(n: WorkNote, lang: Lang): string {
  const ids = (n.phases ?? []).join(", ");
  switch (n.kind) {
    case "merged": return t("twNoteMerged", lang).replace("{ids}", ids).replace("{path}", n.detail ?? "");
    case "tooManyPhases": return t("twNoteTooManyPhases", lang).replace("{n}", n.detail ?? "").replace("{ids}", ids);
    case "tooManyAgents": return t("twNoteTooManyAgents", lang).replace("{ids}", ids).replace("{n}", n.detail ?? "");
    case "unknownAgent": return t("twNoteUnknownAgent", lang).replace("{ids}", ids).replace("{x}", n.detail ?? "");
    case "cycle": return t("twNoteCycle", lang).replace("{ids}", ids);
    case "solo": return t("twNoteSolo", lang);
    case "plannerFallback": return t("twNoteFallback", lang);
    default: return n.kind;
  }
}
