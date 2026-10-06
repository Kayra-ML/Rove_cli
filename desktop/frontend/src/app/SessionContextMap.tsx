import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { Message, Session } from "~/lib/types";
import { useSessions } from "~/hooks/useApi";
import { usePersonaBadges } from "~/hooks/usePersona";
import { spaceOf, type Space } from "~/lib/spaces";
import { usePrefs } from "~/hooks/usePrefs";
import { t, type Lang } from "~/lib/i18n";
import { Markdown } from "~/lib/markdown";
import { NeuralCanvas } from "~/lib/neuralCanvas";
import { toast } from "~/lib/toast";
import { ContextAssistant } from "./ContextAssistant";
import { AgentMark } from "./AgentMark";
import { Icon } from "./Icons";
import { buildSessionContext, windowOf, type MapAction, type CtxItem, type CtxTurn, type SessionContext } from "~/lib/sessionContext";

interface Props {
  // the Chat space's open chat: shown first
  sessionId?: string;
  space?: Space;
  sideTop?: ReactNode;
  onOpenSession?: (s: Session) => void;
}

// one calm color per kind of thing, the same in every chat
const KIND_COLOR: Record<CtxItem["kind"], string> = {
  session: "#c9c9d1",
  prompt: "#7ea6c9",
  file: "#5cc49a",
  command: "#d9a55a",
  web: "#62b6e0",
  agent: "#e0859c",
};
const KIND_KEY: Record<CtxItem["kind"], string> = {
  session: "ctxkSession", prompt: "ctxkPrompt", file: "ctxkFile", command: "ctxkCommand", web: "ctxkWeb", agent: "ctxkAgent",
};
const VIEW_KEY = "aether.ctx.view";

const compact = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(n >= 10_000 ? 0 : 1)}k` : String(n));
const oneLine = (s: string) => s.replace(/\s+/g, " ").trim();

// SessionContextMap shows what one chat's context is made of, in the main
// area where there is room to read it: how full the model's window is and
// with what, each request you made and what the agent drew on to answer it,
// and the files and commands the chat keeps coming back to. It reads the
// chat's history, so any chat has one — no project folder needed.
export function SessionContextMap({ sessionId, space, sideTop, onOpenSession }: Props) {
  const { lang } = usePrefs();
  const { sessions: all, reload: reloadSessions } = useSessions();
  const { badgeFor } = usePersonaBadges();
  const sessions = useMemo(
    () => (space ? all.filter((s) => spaceOf(s, badgeFor(s.id)) === space) : all),
    [all, badgeFor, space],
  );
  const [pick, setPick] = useState(sessionId ?? "");
  const current = sessions.find((s) => s.id === pick) ?? sessions[0] ?? null;
  const [ctx, setCtx] = useState<SessionContext | null>(null);
  const [model, setModel] = useState("");
  const [loading, setLoading] = useState(false);
  // a file or command picked on the side: the requests that used it stand out
  const [focus, setFocus] = useState<string | null>(null);
  const [open, setOpen] = useState<number | null>(null);
  // the graph, or the same context as a readable list
  const [view, setViewState] = useState<"graph" | "list">(() => {
    try { return localStorage.getItem(VIEW_KEY) === "list" ? "list" : "graph"; } catch { return "graph"; }
  });
  const setView = (v: "graph" | "list") => {
    setViewState(v);
    try { localStorage.setItem(VIEW_KEY, v); } catch { /* private */ }
  };
  // the canvas only exists once a chat with turns is shown: build the graph
  // when it appears, not when this view mounts
  const [canvasEl, setCanvasEl] = useState<HTMLCanvasElement | null>(null);
  const graphRef = useRef<NeuralCanvas | null>(null);
  const [graphOn, setGraphOn] = useState(0);
  const [noCanvas, setNoCanvas] = useState(false);
  const [picked, setPicked] = useState<string | null>(null);
  // the assistant at the foot of the sidebar: ask about this chat's context,
  // edit it. Once opened it stays mounted, so tucking it away does not stop
  // an answer that is being written.
  const [asking, setAsking] = useState(false);
  const [opened, setOpened] = useState(false);
  const [caBusy, setCaBusy] = useState(false);
  // an answer finished while the panel was tucked away
  const [caUnread, setCaUnread] = useState(false);
  const askingRef = useRef(asking);
  askingRef.current = asking;
  const onCaBusy = useCallback((b: boolean) => {
    setCaBusy((was) => {
      if (was && !b && !askingRef.current) setCaUnread(true);
      return b;
    });
  }, []);
  const openAsk = () => { setOpened(true); setAsking(true); setCaUnread(false); };

  useEffect(() => {
    if (sessionId) setPick(sessionId);
  }, [sessionId]);

  const load = useCallback(async (s: Session) => {
    setLoading(true);
    try {
      const [history, m] = await Promise.all([
        rpc<Message[]>("session.history", { sessionId: s.id }),
        rpc<{ model?: string }>("session.model", { sessionId: s.id }).catch(() => ({ model: "" })),
      ]);
      setCtx(buildSessionContext(history ?? [], s.id, s.title));
      setModel(m?.model ?? "");
    } catch {
      setCtx(null);
    } finally {
      setLoading(false);
    }
  }, []);

  const currentId = current?.id;
  useEffect(() => {
    setCtx(null);
    setFocus(null);
    setOpen(null);
    // the assistant follows the chat on screen; another chat's state goes
    setCaBusy(false);
    setCaUnread(false);
    if (current) void load(current);
  // the chat's id, not the object: the list refreshes under us
  }, [currentId]); // eslint-disable-line react-hooks/exhaustive-deps

  // the chat keeps talking: refresh when a turn ends
  useEffect(() => {
    if (!current) return;
    let timer = 0;
    const off = subscribeEvents("message.done", (ev) => {
      const p = ev.payload as { sessionId?: string } | undefined;
      if (p?.sessionId && p.sessionId !== current.id) return;
      window.clearTimeout(timer);
      timer = window.setTimeout(() => void load(current), 600);
    });
    return () => { off(); window.clearTimeout(timer); };
  }, [currentId, load]); // eslint-disable-line react-hooks/exhaustive-deps

  // the graph lives while its view is shown
  useEffect(() => {
    const c = canvasEl;
    if (!c) return;
    let g: NeuralCanvas;
    try {
      g = new NeuralCanvas(c, { onSelect: (id) => setPicked(id) });
    } catch {
      setNoCanvas(true);
      return;
    }
    // a chat has tens of nodes, not hundreds: give them room
    g.setForces({ repulsion: 1100, linkDistance: 70, groupGravity: 0.012 });
    graphRef.current = g;
    setGraphOn((n) => n + 1);
    return () => {
      g.destroy();
      graphRef.current = null;
    };
  }, [canvasEl]);

  // The chat is the main hub; a request that drew on something, and a file
  // or command several requests came back to, are mid hubs; hubs are drawn
  // larger.
  useEffect(() => {
    const g = graphRef.current;
    if (!g || view !== "graph") return;
    setPicked(null);
    if (!ctx) {
      g.setData([], []);
      return;
    }
    const nodes = [...ctx.items.values()].map((it) => {
      const tier: 0 | 1 | 2 = it.kind === "session" ? 2
        : it.kind === "prompt" ? (ctx.turns[Number(it.id.slice(2))]?.items.length ?? 0) > 1 ? 1 : 0
        : it.turns.length >= 3 ? 1 : 0;
      return {
        id: it.id,
        label: it.label,
        group: t(KIND_KEY[it.kind], lang),
        rank: tier === 2 ? 10 : tier === 1 ? 2.2 + it.turns.length * 0.15 : 0.8 + it.writes * 0.2,
        loc: 0, in: 0, out: 0,
        tier,
      };
    });
    const colors = new Map((Object.keys(KIND_COLOR) as CtxItem["kind"][]).map((k) => [t(KIND_KEY[k], lang), KIND_COLOR[k]]));
    g.setData(nodes, ctx.edges.map((e) => ({ from: e.from, to: e.to, kind: e.kind === "uses" ? "call" : e.kind })), colors);
    g.fit();
    const id = window.setTimeout(() => g.fit(), 800);
    return () => window.clearTimeout(id);
  }, [ctx, lang, view, graphOn]);

  // picking a file or command brings the first request that used it into view
  useEffect(() => {
    if (!focus) return;
    const el = document.querySelector<HTMLElement>(".cx-turn:not(.dim)");
    el?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [focus]);

  const byKind = useMemo(() => {
    const list = [...(ctx?.items.values() ?? [])];
    const rank = (a: CtxItem, b: CtxItem) => b.writes - a.writes || b.turns.length - a.turns.length || b.runs - a.runs;
    return {
      files: list.filter((it) => it.kind === "file").sort(rank),
      commands: list.filter((it) => it.kind === "command").sort(rank),
      others: list.filter((it) => it.kind === "agent" || it.kind === "web").sort(rank),
    };
  }, [ctx]);

  const win = windowOf(model);
  const pickedItem = ctx ? ctx.items.get(picked ?? focus ?? "") ?? null : null;

  // The assistant is talking about these: glide there, framed with room to
  // spare, and light them up. In the list, open the first request named.
  const follow = useCallback((ids: string[]) => {
    if (view === "graph") {
      graphRef.current?.focusOn(ids, { minZoom: 1.15, maxZoom: 1.8, pad: 80 });
      return;
    }
    const first = ids.find((id) => id.startsWith("p:"));
    if (first) {
      const n = Number(first.slice(2));
      setOpen(n);
      document.querySelectorAll<HTMLElement>(".cx-turn")[n]?.scrollIntoView({ block: "center", behavior: "smooth" });
    }
  }, [view]);

  // An edit the assistant offered and the user confirmed. "show" points at
  // something on the map; the rest change the chat, and the map reloads.
  const act = async (a: MapAction) => {
    if (!current || !ctx) return;
    if (a.action === "show") {
      const want = a.file ?? "";
      const id = a.turn
        ? `p:${a.turn - 1}`
        : [...ctx.items.values()].find((it) => it.kind === "file" && (it.detail === want || it.detail.endsWith(`/${want}`) || it.label === want))?.id;
      if (!id || !ctx.items.has(id)) {
        toast(t("caNotFound", lang), "err");
        return;
      }
      if (view === "graph") {
        setPicked(id);
        graphRef.current?.select(id, true);
      } else if (a.turn) {
        setOpen(a.turn - 1);
        document.querySelectorAll<HTMLElement>(".cx-turn")[a.turn - 1]?.scrollIntoView({ block: "center", behavior: "smooth" });
      } else {
        setFocus(id);
      }
      return;
    }
    if (a.action === "compact") {
      const r = await rpc<{ compacted?: boolean }>("session.compact", { sessionId: current.id });
      toast(t(r?.compacted ? "caCompacted" : "caNothingToCompact", lang), r?.compacted ? "ok" : undefined);
    } else if (a.action === "forget") {
      const tn = ctx.turns[a.fromTurn - 1];
      if (!tn) throw new Error(t("caNotFound", lang));
      await rpc("session.truncate", { sessionId: current.id, keep: tn.msgIndex });
      toast(`${a.fromTurn}. ${t("caForgotten", lang)}`, "ok");
    } else if (a.action === "rename") {
      await rpc("session.rename", { id: current.id, title: a.title });
      await reloadSessions();
      toast(t("caRenamed", lang), "ok");
    }
    await load(current);
  };
  const biggest = Math.max(1, ...(ctx?.turns.map((x) => x.tokens) ?? [1]));

  return (
    <div className="map">
      <div className="cx-wrap">
      <div className={`cx${view === "graph" ? " cx-graphmode" : ""}`}>
        {!current && <div className="cx-empty">{t("ctxNoChats", lang)}</div>}
        {current && ctx && (
          <>
            <header className="cx-head">
              <div className="cx-title-row">
                <div>
                  <h2 className="cx-title">{current.title || t("chat", lang)}</h2>
                  <div className="cx-sub">
                    {[model, `${ctx.turns.length} ${t("ctxTurns", lang)}`, `${ctx.toolCalls} ${t("ctxTools", lang)}`, `${ctx.messages} ${t("ctxMessages", lang)}`].filter(Boolean).join(" · ")}
                  </div>
                </div>
                <div className="cx-head-acts">
                  <div className="seg cx-view" role="radiogroup" aria-label={t("ctxView", lang)}>
                    {(["graph", "list"] as const).map((v) => (
                      <button key={v} type="button" role="radio" aria-checked={view === v} className={view === v ? "on" : ""} onClick={() => setView(v)}>
                        {t(v === "graph" ? "ctxViewGraph" : "ctxViewList", lang)}
                      </button>
                    ))}
                  </div>
                  {view === "graph" && <button type="button" onClick={() => graphRef.current?.fit()}>{t("mapFit", lang)}</button>}
                  <button type="button" onClick={() => onOpenSession?.(current)}>{t("ctxOpenChat", lang)}</button>
                </div>
              </div>
              <WindowBar ctx={ctx} win={win} lang={lang} />
              {ctx.compacted && <div className="cx-note">{t("ctxCompacted", lang)}</div>}
            </header>

            {ctx.turns.length === 0 ? (
              <div className="cx-empty">{t("ctxEmptyChat", lang)}</div>
            ) : view === "graph" ? (
              <div className="cx-graph">
                <canvas ref={setCanvasEl} className="map-canvas" />
                <div className="map-legend">
                  {(Object.keys(KIND_COLOR) as CtxItem["kind"][]).filter((k) => [...ctx.items.values()].some((it) => it.kind === k)).map((k) => (
                    <span key={k} className="map-legend-item"><i style={{ background: KIND_COLOR[k] }} />{t(KIND_KEY[k], lang)}</span>
                  ))}
                  <span className="map-legend-item cx-legend-note">{t("ctxColorsOnPick", lang)}</span>
                </div>
                <div className="map-hint">{t("mapHint", lang)}</div>
                {noCanvas && <div className="map-empty">{t("mapNoCanvas", lang)}</div>}
                {picked && ctx.items.get(picked) && ctx.items.get(picked)!.kind !== "session" && (
                  <GraphCard item={ctx.items.get(picked)!} ctx={ctx} lang={lang}
                    onClose={() => { setPicked(null); graphRef.current?.select(null); }}
                    onPick={(id) => { setPicked(id); graphRef.current?.select(id, true); }} />
                )}
              </div>
            ) : (
              <div className="cx-body">
                <section className="cx-flow" aria-label={t("ctxkPrompt", lang)}>
                  <div className="cx-label">
                    {t("ctxkPrompt", lang)}
                    {focus && (
                      <button type="button" className="ghost map-mini" onClick={() => setFocus(null)}>
                        {ctx.items.get(focus)?.label} ×
                      </button>
                    )}
                  </div>
                  {ctx.turns.map((tn) => (
                    <Turn
                      key={tn.index}
                      turn={tn}
                      ctx={ctx}
                      biggest={biggest}
                      dim={Boolean(focus && !tn.items.includes(focus))}
                      open={open === tn.index}
                      onToggle={() => setOpen(open === tn.index ? null : tn.index)}
                      onItem={(id) => setFocus(focus === id ? null : id)}
                      focus={focus}
                      lang={lang}
                    />
                  ))}
                </section>
                <aside className="cx-side">
                  <ItemList title={t("ctxkFile", lang)} items={byKind.files} focus={focus} onPick={setFocus} lang={lang} />
                  <ItemList title={t("ctxkCommand", lang)} items={byKind.commands} focus={focus} onPick={setFocus} lang={lang} />
                  <ItemList title={t("ctxkAgent", lang)} items={byKind.others} focus={focus} onPick={setFocus} lang={lang} />
                </aside>
              </div>
            )}
          </>
        )}
        {current && !ctx && loading && <div className="cx-empty">…</div>}
      </div>
      </div>

      <aside className={`map-side cx-side-col${opened && asking ? " with-ca" : ""}`}>
        {sideTop}
        <section className="map-convs cx-convs">
          <div className="section-label">{t("convs", lang)}</div>
          {sessions.length === 0 && <div className="map-muted">{t("ctxNoSessions", lang)}</div>}
          {sessions.map((s) => (
            <button
              key={s.id}
              type="button"
              className={`map-row map-conv${s.id === current?.id ? " active" : ""}`}
              aria-pressed={s.id === current?.id}
              onClick={() => setPick(s.id)}
              onDoubleClick={() => onOpenSession?.(s)}
              title={s.title}
            >
              {(() => {
                // an Agent-space chat: its agent's logo beside the chat's name
                const b = badgeFor(s.id);
                return b ? <AgentMark mark={b.mark} color={b.color} seed={b.profileId || b.characterId || s.id} character={b.characterId} size={16} title={b.name} /> : null;
              })()}
              <span className="map-file">{s.title || s.id}</span>
            </button>
          ))}
        </section>
        {current && ctx && opened && (
          <ContextAssistant key={current.id} chat={current} ctx={ctx} model={model} win={win} picked={pickedItem} lang={lang}
            hidden={!asking} onHide={() => setAsking(false)} onAction={act} onBusy={onCaBusy} onRefs={follow} />
        )}
        {current && ctx && !asking && (
          <button type="button" className={`ca-launch${caBusy ? " busy" : ""}${caUnread ? " unread" : ""}`} onClick={openAsk} title={t("caOpenHint", lang)}>
            <Icon name="lightning" size={13} />
            <span>{t("caOpen", lang)}</span>
            {caBusy ? <span className="ca-launch-state"><i className="ca-spin" />{t("caWorking", lang)}</span>
              : caUnread ? <span className="ca-launch-state">● {t("caReady", lang)}</span> : null}
          </button>
        )}
      </aside>
    </div>
  );
}

// WindowBar is how full the model's window is, split by what fills it.
function WindowBar({ ctx, win, lang }: { ctx: SessionContext; win: number; lang: Lang }) {
  const segs = [
    { key: "summary", label: t("ctxPartSummary", lang), n: ctx.parts.summary },
    { key: "user", label: t("ctxPartUser", lang), n: ctx.parts.user },
    { key: "assistant", label: t("ctxPartAssistant", lang), n: ctx.parts.assistant },
    { key: "tools", label: t("ctxPartTools", lang), n: ctx.parts.tools },
  ].filter((s) => s.n > 0);
  const pct = Math.min(100, (ctx.liveTokens / win) * 100);
  return (
    <div className="cx-window" title={t("ctxMeterHint", lang)}>
      <div className="cx-window-top">
        <span>{t("ctxInWindow", lang)}</span>
        <span className="cx-num">≈{compact(ctx.liveTokens)} / {compact(win)} · %{pct < 1 && pct > 0 ? "<1" : Math.round(pct)}</span>
      </div>
      <div className="cx-window-bar">
        {segs.map((s) => (
          <i key={s.key} className={`cx-seg ${s.key}`} style={{ width: `${Math.max(0.4, (s.n / win) * 100)}%` }} />
        ))}
      </div>
      <div className="cx-window-legend">
        {segs.map((s) => (
          <span key={s.key}><i className={`cx-seg ${s.key}`} />{s.label} <b>≈{compact(s.n)}</b></span>
        ))}
      </div>
    </div>
  );
}

function Chip({ it, on, onClick }: { it: CtxItem; on: boolean; onClick: () => void }) {
  const mark = it.kind === "file" ? (it.writes ? "✎" : "◦") : it.kind === "command" ? "$" : "↗";
  return (
    <button type="button" className={`cx-chip ${it.kind}${on ? " on" : ""}${it.errors ? " bad" : ""}`} title={it.detail} onClick={(e) => { e.stopPropagation(); onClick(); }}>
      <span className="cx-chip-mark">{mark}</span>{it.label}
    </button>
  );
}

function Turn({ turn, ctx, biggest, dim, open, onToggle, onItem, focus, lang }: {
  turn: CtxTurn; ctx: SessionContext; biggest: number; dim: boolean; open: boolean;
  onToggle: () => void; onItem: (id: string) => void; focus: string | null; lang: Lang;
}) {
  const used = turn.items.filter((id) => !id.startsWith("p:")).map((id) => ctx.items.get(id)!).filter(Boolean);
  return (
    <div className={`cx-turn${dim ? " dim" : ""}${open ? " open" : ""}`} onClick={onToggle}>
      <span className="cx-turn-n">{turn.index + 1}</span>
      <div className="cx-turn-main">
        <div className="cx-turn-prompt">{open ? turn.prompt : oneLine(turn.prompt)}</div>
        <div className="cx-turn-meta">
          <span className="cx-turn-bar"><i style={{ width: `${Math.max(3, (turn.tokens / biggest) * 100)}%` }} /></span>
          <span>≈{compact(turn.tokens)} token</span>
          <span>{new Date(turn.at).toLocaleTimeString(lang === "tr" ? "tr-TR" : undefined, { hour: "2-digit", minute: "2-digit" })}</span>
        </div>
        {used.length > 0 && (
          <div className="cx-chips">
            {used.map((it) => <Chip key={it.id} it={it} on={focus === it.id} onClick={() => onItem(it.id)} />)}
          </div>
        )}
        {open && turn.answer && (
          <div className="cx-answer">
            <div className="cx-label">{t("ctxAnswer", lang)}</div>
            <div className="cx-answer-body" onClick={(e) => e.stopPropagation()}>
              <Markdown text={turn.answer.length > 2400 ? `${turn.answer.slice(0, 2400)}…` : turn.answer} />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function ItemList({ title, items, focus, onPick, lang }: {
  title: string; items: CtxItem[]; focus: string | null; onPick: (id: string | null) => void; lang: Lang;
}) {
  if (items.length === 0) return null;
  return (
    <section className="cx-list">
      <div className="cx-label">{title} <span className="cx-count">{items.length}</span></div>
      {items.slice(0, 30).map((it) => (
        <button key={it.id} type="button" className={`cx-item ${it.kind}${focus === it.id ? " on" : ""}`} title={it.detail} onClick={() => onPick(focus === it.id ? null : it.id)}>
          <span className="cx-item-name">{it.label}</span>
          <span className="cx-item-meta">
            {it.writes > 0 && <span className="w">✎{it.writes}</span>}
            {it.reads > 0 && <span>◦{it.reads}</span>}
            {it.runs > 0 && <span>${it.runs}</span>}
            {it.errors > 0 && <span className="bad">!{it.errors}</span>}
          </span>
          {it.kind === "file" && it.detail !== it.label && <span className="cx-item-path">{it.detail}</span>}
          {it.turns.length > 1 && <span className="cx-item-turns">{it.turns.length} {t("ctxTurns", lang)}</span>}
        </button>
      ))}
    </section>
  );
}

// GraphCard is what a picked node stands for, over the graph: a request's
// words and what it drew on, or a file's or command's history in the chat.
function GraphCard({ item, ctx, lang, onClose, onPick }: {
  item: CtxItem; ctx: SessionContext; lang: Lang; onClose: () => void; onPick: (id: string) => void;
}) {
  const turn = item.kind === "prompt" ? ctx.turns[Number(item.id.slice(2))] : undefined;
  const used = turn ? turn.items.filter((id) => id !== item.id).map((id) => ctx.items.get(id)!).filter(Boolean) : [];
  return (
    <div className="cx-card" onPointerDown={(e) => e.stopPropagation()} onWheel={(e) => e.stopPropagation()}>
      <div className="cx-card-head">
        <span className="cx-card-kind"><i style={{ background: KIND_COLOR[item.kind] }} />{t(KIND_KEY[item.kind], lang)}</span>
        <button type="button" className="icon-btn" aria-label={t("close", lang)} onClick={onClose}>×</button>
      </div>
      {turn ? (
        <>
          <div className="cx-card-text">{turn.prompt}</div>
          <div className="cx-sub">≈{compact(turn.tokens)} token · {new Date(turn.at).toLocaleString(lang === "tr" ? "tr-TR" : undefined)}</div>
          {used.length > 0 && (
            <div className="cx-chips">
              {used.map((it) => <Chip key={it.id} it={it} on={false} onClick={() => onPick(it.id)} />)}
            </div>
          )}
          {turn.answer && (
            <div className="cx-answer">
              <div className="cx-label">{t("ctxAnswer", lang)}</div>
              <div className="cx-answer-body"><Markdown text={turn.answer.length > 1600 ? `${turn.answer.slice(0, 1600)}…` : turn.answer} /></div>
            </div>
          )}
        </>
      ) : (
        <>
          <div className="cx-card-title" title={item.detail}>{item.label}</div>
          {item.detail !== item.label && <div className="cx-item-path">{item.detail}</div>}
          <div className="cx-sub">{[item.writes && `✎ ${item.writes}`, item.reads && `◦ ${item.reads}`, item.runs && `$ ${item.runs}`, item.errors && `! ${item.errors}`].filter(Boolean).join("   ")}</div>
          {item.last && <pre className="ctx-last">{item.last}</pre>}
          <div className="cx-label">{t("ctxInTurns", lang)} ({item.turns.length})</div>
          <div className="cx-card-turns">
            {item.turns.map((i) => (
              <button key={i} type="button" className="map-row" onClick={() => onPick(`p:${i}`)}>
                <span className="map-file">{i + 1}. {oneLine(ctx.turns[i]?.prompt ?? "")}</span>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
