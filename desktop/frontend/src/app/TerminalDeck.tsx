import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { Session, TerminalSession, Workspace } from "~/lib/types";
import { useWorkspaces } from "~/hooks/useApi";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { Chat } from "./Chat";
import { Icon } from "./Icons";
import { Terminal } from "./Terminal";

const SPLIT_KEY = "aether.term.split";
// how many panes the deck is split into, picked at its top right
const SPLITS = [1, 2, 4, 6, 8] as const;
type Split = (typeof SPLITS)[number];
const MAX_PANES = 8;
// the session's own terminal (T1) always has this key, so its conversation
// stays mounted while a draft becomes the session
const MAIN = "main";

// A slot of the deck: the session itself (T1), another terminal of the
// session, a real shell, or a new terminal not written to yet.
type Pane =
  | { key: string; kind: "main" }
  | { key: string; kind: "term"; sessionId: string }
  | { key: string; kind: "shell"; termId: string }
  | { key: string; kind: "draft" };

// what the server keeps per session (terminal.layout.*)
type SavedLayout = { split?: number; panes?: { kind: "chat" | "shell"; sessionId?: string; termId?: string }[] };

// a terminal of the session, as the daemon lists it
type PaneInfo = { n: number; id: string; label: string; title: string; role?: string; characterId?: string; running: boolean; task?: string };

// splitFor is the smallest split that holds n panes.
export function splitFor(n: number): Split {
  return SPLITS.find((x) => x >= n) ?? MAX_PANES;
}

function defaultSplit(): Split {
  try {
    const n = Number(localStorage.getItem(SPLIT_KEY));
    if ((SPLITS as readonly number[]).includes(n)) return n as Split;
  } catch { /* private */ }
  return 1;
}

interface Props {
  activeSession: Session | null;
  // the Chat space shows a new chat that has no session yet
  draft?: boolean;
  onCreateSession?: () => Promise<Session>;
  workspaceId?: string;
  onFocusSession: (s: Session) => void;
  onActivity?: (map: Record<string, string>) => void;
  onOpenMap?: () => void;
  onOpenContext?: () => void;
  // A shell just opened elsewhere (Settings → Sunucular): take it, give it a
  // pane, and tell the caller it was consumed.
  incomingShell?: TerminalSession | null;
  onConsumedShell?: () => void;
  themeKey?: string;
  // /new in T1: a new session (the app's new draft)
  onNewSession?: () => void;
}

let paneSeq = 0;
const newKey = () => `p${Date.now().toString(36)}${paneSeq++}`;

// TerminalDeck is terminal mode: one session split into 1–8 terminals that
// work as a team. The session itself is T1; the others are its terminals,
// each with its own agent and role. They see what the others do, do not
// edit a file another is editing, and can hand each other work (/to, @T2).
// The layout is kept with the session.
export function TerminalDeck({
  activeSession, draft, onCreateSession, workspaceId, onFocusSession, onActivity, onOpenMap, onOpenContext,
  incomingShell, onConsumedShell, themeKey, onNewSession,
}: Props) {
  const { lang } = usePrefs();
  const { workspaces } = useWorkspaces();
  const [group, setGroup] = useState<Session | null>(activeSession);
  const [infos, setInfos] = useState<PaneInfo[]>([]);
  const [shells, setShells] = useState<TerminalSession[]>([]);
  const [panes, setPanes] = useState<Pane[]>([{ key: MAIN, kind: "main" }]);
  const [split, setSplitState] = useState<Split>(defaultSplit);
  const [focus, setFocus] = useState(0);
  const [maxed, setMaxed] = useState<string | null>(null);
  const [menu, setMenu] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  // set while a draft becomes the session: keep the deck as it is
  const adopting = useRef(false);

  const byShellId = useMemo(() => new Map(shells.map((s) => [s.id, s])), [shells]);
  const infoById = useMemo(() => new Map(infos.map((p) => [p.id, p])), [infos]);
  const wsById = useMemo(() => new Map(workspaces.map((w: Workspace) => [w.id, w])), [workspaces]);

  const refreshInfos = useCallback(async (sid?: string) => {
    const id = sid ?? group?.id;
    if (!id) { setInfos([]); return []; }
    const list = (await rpc<PaneInfo[]>("terminal.panes", { sessionId: id }).catch(() => [])) ?? [];
    setInfos(list);
    return list;
  }, [group?.id]);

  useEffect(() => {
    rpc<TerminalSession[]>("terminal.list").then((l) => setShells(l ?? [])).catch(() => setShells([]));
  }, []);

  // the deck follows the sidebar: another session brings its own terminals
  useEffect(() => {
    if (adopting.current && activeSession) {
      adopting.current = false;
      setGroup(activeSession);
      setLoaded(true);
      return;
    }
    setGroup(activeSession);
    setMaxed(null);
    setFocus(0);
    if (!activeSession) {
      setInfos([]);
      setPanes([{ key: MAIN, kind: "main" }]);
      setSplitState(defaultSplit());
      setLoaded(true);
      return;
    }
    let live = true;
    setLoaded(false);
    void (async () => {
      const [saved, list, shellList] = await Promise.all([
        rpc<SavedLayout | null>("terminal.layout.get", { sessionId: activeSession.id }).catch(() => null),
        rpc<PaneInfo[]>("terminal.panes", { sessionId: activeSession.id }).catch(() => []),
        rpc<TerminalSession[]>("terminal.list").catch(() => []),
      ]);
      if (!live) return;
      const known = new Set((list ?? []).map((p) => p.id));
      const shellIds = new Set((shellList ?? []).map((s) => s.id));
      const next: Pane[] = [{ key: MAIN, kind: "main" }];
      for (const p of saved?.panes ?? []) {
        if (p.kind === "chat" && p.sessionId && p.sessionId !== activeSession.id && known.has(p.sessionId)) next.push({ key: newKey(), kind: "term", sessionId: p.sessionId });
        if (p.kind === "shell" && p.termId && shellIds.has(p.termId)) next.push({ key: newKey(), kind: "shell", termId: p.termId });
      }
      setInfos(list ?? []);
      setShells(shellList ?? []);
      setPanes(next.slice(0, MAX_PANES));
      const n = Number(saved?.split);
      setSplitState((SPLITS as readonly number[]).includes(n) ? (n as Split) : splitFor(next.length));
      setLoaded(true);
    })();
    return () => { live = false; };
  }, [activeSession?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // a pane added past the split grows it; free slots open as new terminals
  useEffect(() => {
    if (panes.length > split) setSplitState(splitFor(panes.length));
  }, [panes.length, split]);
  useEffect(() => {
    if (!loaded || panes.length >= split) return;
    setPanes((ps) => (ps.length >= split ? ps : [...ps, ...Array.from({ length: split - ps.length }, (): Pane => ({ key: newKey(), kind: "draft" }))]));
  }, [panes.length, split, loaded]);
  useEffect(() => {
    try { localStorage.setItem(SPLIT_KEY, String(split)); } catch { /* private */ }
  }, [split]);

  // the layout is kept with the session (drafts are not: never written to)
  useEffect(() => {
    if (!group || !loaded) return;
    const layout: SavedLayout = {
      split,
      panes: panes.flatMap((p): NonNullable<SavedLayout["panes"]> => p.kind === "main" ? [{ kind: "chat", sessionId: group.id }]
        : p.kind === "term" ? [{ kind: "chat", sessionId: p.sessionId }]
        : p.kind === "shell" ? [{ kind: "shell", termId: p.termId }] : []),
    };
    const id = window.setTimeout(() => {
      void rpc("terminal.layout.set", { sessionId: group.id, layout }).then(() => refreshInfos(group.id)).catch(() => {});
    }, 300);
    return () => window.clearTimeout(id);
  }, [group?.id, panes, split, loaded]); // eslint-disable-line react-hooks/exhaustive-deps

  // who is working: follow runs of this session's terminals
  useEffect(() => {
    if (!group) return;
    const ids = new Set(infos.map((p) => p.id));
    const on = (ev: { type: string; payload?: unknown }) => {
      const sid = (ev.payload as { sessionId?: string } | undefined)?.sessionId;
      if (!sid || !ids.has(sid)) return;
      setInfos((list) => list.map((p) => (p.id === sid ? { ...p, running: ev.type !== "run.done" } : p)));
      if (ev.type === "run.done") void refreshInfos();
    };
    const offs = [subscribeEvents("message.delta", on), subscribeEvents("tool.start", on), subscribeEvents("run.done", on)];
    return () => offs.forEach((f) => f());
  }, [group?.id, infos.length]); // eslint-disable-line react-hooks/exhaustive-deps

  // a shell opened from Settings lands in its own pane here
  useEffect(() => {
    if (!incomingShell) return;
    setShells((s) => (s.some((x) => x.id === incomingShell.id) ? s : [...s, incomingShell]));
    placePane({ key: newKey(), kind: "shell", termId: incomingShell.id });
    onConsumedShell?.();
  }, [incomingShell]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenu(null);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  // placePane puts a pane into a free new-terminal slot, else adds one
  function placePane(pane: Pane) {
    setMaxed(null);
    setPanes((ps) => {
      const free = ps[focus]?.kind === "draft" ? focus : ps.findIndex((p) => p.kind === "draft");
      if (free >= 0) {
        setFocus(free);
        return ps.map((p, j) => (j === free ? pane : p));
      }
      if (ps.length >= MAX_PANES) return ps;
      setFocus(ps.length);
      return [...ps, pane];
    });
  }

  // the session this deck belongs to; a draft deck makes it on first use
  async function ensureGroup(): Promise<Session> {
    if (group) return group;
    if (!onCreateSession) throw new Error("no session");
    adopting.current = true;
    const s = await onCreateSession();
    setGroup(s);
    return s;
  }

  // a new terminal's first message opens it as a terminal of the session;
  // the pane keeps its key so its conversation stays mounted
  const createFor = (key: string) => async (): Promise<Session> => {
    if (key === MAIN) {
      if (group) return group;
      return ensureGroup();
    }
    const parent = await ensureGroup();
    const child = await rpc<Session>("terminal.newPane", { parentId: parent.id });
    setPanes((ps) => ps.map((p) => (p.key === key ? { key, kind: "term", sessionId: child.id } : p)));
    await refreshInfos(parent.id);
    return child;
  };

  // setSplit picks how many panes the page is split into; panes past the
  // new size leave the view (terminals stay in the session, shells keep
  // running) — empty new terminals go first
  const setSplit = (n: Split) => {
    setMaxed(null);
    setSplitState(n);
    if (panes.length <= n) return;
    let keep = [...panes];
    for (let i = keep.length - 1; i >= 1 && keep.length > n; i--) {
      if (keep[i].kind === "draft") keep.splice(i, 1);
    }
    for (const p of keep.slice(n)) {
      if (p.kind === "shell") void rpc("terminal.detach", { id: p.termId }).catch(() => {});
    }
    keep = keep.slice(0, n);
    setPanes(keep);
    setFocus((f) => Math.min(f, n - 1));
  };

  const addShellPane = async (termId?: string) => {
    setMenu(null);
    let sh: TerminalSession | undefined = termId ? byShellId.get(termId) : undefined;
    if (!sh) {
      const ws = group?.workspaceId ?? workspaceId;
      try {
        sh = await rpc<TerminalSession>("terminal.spawn", { kind: "user", cwd: ws ? wsById.get(ws)?.path ?? "" : "" });
      } catch {
        return;
      }
      setShells((s) => [...s, sh as TerminalSession]);
    }
    placePane({ key: newKey(), kind: "shell", termId: sh.id });
  };

  const restartShell = useCallback(async (id: string) => {
    try {
      const sess = await rpc<TerminalSession>("terminal.restart", { id });
      setShells((s) => s.map((x) => (x.id === id ? { ...x, ...sess, id } : x)));
    } catch { /* ignore */ }
  }, []);

  // closing a pane empties its slot back to a new terminal; a closed
  // terminal stays in the session (reopen it from +), T1 cannot close
  const closePane = (i: number) => {
    const p = panes[i];
    if (p.kind === "draft" || p.kind === "main") return;
    if (p.kind === "shell") void rpc("terminal.detach", { id: p.termId }).catch(() => {});
    setPanes(panes.map((x, j) => (j === i ? { key: newKey(), kind: "draft" } : x)));
    if (maxed === p.key) setMaxed(null);
    setFocus(i);
  };

  // /new in a pane: T1 starts a new session; another pane a new terminal
  const freshPane = (key: string) => {
    setMaxed(null);
    if (key === MAIN) {
      onNewSession?.();
      return;
    }
    setPanes((ps) => ps.map((x) => (x.key === key ? { key: newKey(), kind: "draft" } : x)));
  };

  const maxedPane = maxed ? panes.find((p) => p.key === maxed) : undefined;
  const shown = maxedPane ? [maxedPane] : panes;
  const slots = maxedPane ? 1 : Math.max(split, panes.length);
  // terminals of this session that are not on screen
  const closedTerms = infos.filter((p) => p.id !== group?.id && !panes.some((x) => x.kind === "term" && x.sessionId === p.id));
  const freeShells = shells.filter((sh) => !panes.some((p) => p.kind === "shell" && p.termId === sh.id));

  const shellName = (sh: TerminalSession) => (sh.ssh ? (sh.ssh.user ? `${sh.ssh.user}@${sh.ssh.host}` : sh.ssh.host) : sh.title || t("rpShell", lang));
  // what a pane's + menu opens
  const openers = () => (
    <>
      <button type="button" onClick={() => { setMenu(null); placePane({ key: newKey(), kind: "draft" }); }}>+ {t("termNewTerminal", lang)}</button>
      {closedTerms.map((p) => (
        <button type="button" key={p.id} onClick={() => { setMenu(null); placePane({ key: newKey(), kind: "term", sessionId: p.id }); }}>
          <span className="pane-label">{p.label}</span> {p.title}
        </button>
      ))}
      <button type="button" onClick={() => void addShellPane()}><Icon name="terminal" size={12} /> {t("rpNewShell", lang)}</button>
      {freeShells.slice(0, 8).map((fsh) => (
        <button type="button" key={fsh.id} onClick={() => void addShellPane(fsh.id)}>
          <Icon name="terminal" size={12} /> {shellName(fsh)}
        </button>
      ))}
    </>
  );

  // a pane's label: T1 for the session, T2… for its terminals, by the daemon
  const labelOf = (sid: string | undefined) => (sid ? infoById.get(sid)?.label : undefined);
  const sessionOf = (sid: string): Session | null => {
    if (group && sid === group.id) return group;
    const info = infoById.get(sid);
    if (!info || !group) return null;
    return { id: sid, title: info.title, agentId: group.agentId, workspaceId: group.workspaceId, parentId: group.id, updatedAt: group.updatedAt };
  };

  const bar = (
    <div className="deck-bar">
      {group && (
        // the daemon names a session after its first message: its list is fresher than ours
        <span className="deck-title" title={infoById.get(group.id)?.title || group.title}>{infoById.get(group.id)?.title || group.title || t("termNewChat", lang)}</span>
      )}
      <span className="pane-spacer" />
      <span className="deck-count">{panes.filter((p) => p.kind !== "draft").length}/{slots}</span>
      <div className="deck-split" role="radiogroup" aria-label={t("termSplitLabel", lang)}>
        {SPLITS.map((n) => (
          <button
            key={n}
            type="button"
            role="radio"
            aria-checked={split === n && !maxedPane}
            className={split === n && !maxedPane ? "on" : ""}
            title={`${n} ${t("termPanes", lang)}`}
            onClick={() => setSplit(n)}
          >
            <SplitIcon n={n} />
            <span>{n}</span>
          </button>
        ))}
      </div>
    </div>
  );

  return (
    <div className="deck-wrap">
      {bar}
      <div className={`deck split-${slots}`}>
        {shown.map((p) => {
          const i = panes.indexOf(p);
          const sid = p.kind === "main" ? group?.id : p.kind === "term" ? p.sessionId : undefined;
          const s = sid ? sessionOf(sid) : null;
          const sh = p.kind === "shell" ? byShellId.get(p.termId) : null;
          const ws = s ? wsById.get(s.workspaceId) : undefined;
          const info = sid ? infoById.get(sid) : undefined;
          const label = p.kind === "main" ? "T1" : labelOf(sid);
          const chatProps = {
            variant: "terminal" as const,
            onFocusPane: () => i !== focus && setFocus(i),
            onSession: (ns: Session) => onFocusSession(ns), // /session switches the whole deck
            onNewDraft: () => freshPane(p.key),
            onOpenMap,
            onOpenContext,
            pane: { label: label ?? "", groupId: group?.id ?? "" },
          };
          return (
            <section key={p.key} className={`pane panel${i === focus ? " focused" : ""}`} data-pane={label ?? ""} onMouseDown={() => i !== focus && setFocus(i)}>
              <header className="pane-head">
                {p.kind === "shell" ? (
                  <>
                    <span className={`tree-dot${sh?.status === "running" ? " on" : ""}`} />
                    <Icon name="terminal" size={13} />
                    <span className="pane-title">{sh ? shellName(sh) : t("rpShell", lang)}</span>
                    {sh?.ssh && <span className="pane-ws">:{sh.ssh.port ?? 22}</span>}
                  </>
                ) : (
                  <>
                    <span className={`tree-dot${info?.running ? " live" : " on"}`} />
                    {label && <span className="pane-label">{label}</span>}
                    <span className="pane-title">{p.kind === "draft" || !s ? t("termNewTerminal", lang) : s.title || t("termTerminal", lang)}</span>
                    {s && p.kind !== "main" && info?.task && <span className="pane-ws" title={info.task}>{info.task}</span>}
                    {p.kind === "main" && ws && <span className="pane-ws">{ws.name}</span>}
                  </>
                )}
                <span className="pane-spacer" />
                <div className="pane-actions" ref={menu === p.key ? menuRef : undefined}>
                  <button type="button" title={t("termSplit", lang)} disabled={panes.length >= MAX_PANES && !panes.some((x) => x.kind === "draft")} onClick={() => setMenu(menu === p.key ? null : p.key)}>+</button>
                  <button type="button" title={t("termMaximize", lang)} onClick={() => setMaxed(maxed === p.key ? null : p.key)}>{maxed === p.key ? "⤡" : "⤢"}</button>
                  {p.kind !== "draft" && p.kind !== "main" && <button type="button" title={t("close", lang)} onClick={() => closePane(i)}>×</button>}
                  {menu === p.key && <div className="pane-menu">{openers()}</div>}
                </div>
              </header>
              {p.kind !== "shell" && (
                // one element per pane: a draft turning into its terminal
                // keeps the same conversation mounted
                <Chat
                  key={p.key}
                  {...chatProps}
                  session={s}
                  draft={!s}
                  onCreateSession={createFor(p.key)}
                  workspaceId={s?.workspaceId ?? group?.workspaceId ?? workspaceId}
                  workspacePath={ws?.path}
                  onActivity={i === focus ? onActivity : undefined}
                />
              )}
              {sh && (
                <Terminal
                  key={sh.id}
                  sessions={[sh]}
                  activeId={sh.id}
                  onSelect={() => {}}
                  onSpawn={() => void addShellPane()}
                  onRestart={(id) => void restartShell(id)}
                  onDetach={() => closePane(i)}
                  hideTabs
                  themeKey={themeKey}
                />
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}

// SplitIcon draws a split's grid: n cells in 1, 2 (side by side) or two rows.
function SplitIcon({ n }: { n: number }) {
  const cols = n <= 2 ? n : n / 2;
  const rows = n <= 2 ? 1 : 2;
  const w = 14, h = 10, gap = 1.4;
  const cw = (w - gap * (cols - 1)) / cols, ch = (h - gap * (rows - 1)) / rows;
  const cells = [];
  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < cols; c++) {
      cells.push(<rect key={`${r}${c}`} x={c * (cw + gap)} y={r * (ch + gap)} width={cw} height={ch} rx={1} />);
    }
  }
  return <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} fill="currentColor" aria-hidden="true">{cells}</svg>;
}
