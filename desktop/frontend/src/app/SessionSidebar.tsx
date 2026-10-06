import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { pickFolder, rpc, subscribeEvents } from "~/lib/rpc";
import type { Session, Workspace } from "~/lib/types";
import { useWorkspaces } from "~/hooks/useApi";
import { usePersonaBadges } from "~/hooks/usePersona";
import { useProfiles } from "~/hooks/useApi";
import { chatsByProfile } from "~/lib/agents";
import { moveOffice } from "~/lib/office";
import { CHAT_MODES, spaceOf, type ChatMode, type Space } from "~/lib/spaces";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { pollWhileVisible } from "~/lib/poll";
import { toast } from "~/lib/toast";
import { Icon } from "./Icons";
import { AgentMark } from "./AgentMark";
import type { OfficeIntent } from "./OfficePanel";
import { compact, type UsageSnapshot } from "./UsageCard";

interface Props {
  // Office shows agents (each row is the chat with that agent); Chat shows
  // sessions and the mode switch.
  space: Space;
  workspace: Workspace | null;
  onSelectWorkspace: (ws: Workspace) => void;
  activeSessionId: string | null;
  onSelectSession: (s: Session) => void;
  // true while a new chat is open but not sent yet
  draft: boolean;
  // the agent (catalog character id) the open draft will talk to
  draftAgent: string | null;
  // opens a draft, with an agent or without, and (in Code) in a chosen
  // mode; the session is only created by its first message
  onNewChat: (characterId?: string | null, mode?: ChatMode) => void;
  // Agent: back to the team's board
  onBoard?: () => void;
  rpcOk: boolean;
  dark: boolean;
  onToggleTheme: () => void;
  onOpenMarket: () => void;
  onOpenSettings: () => void;
  // Office: agents are added and edited in Settings → Office
  onOfficeAgent?: (intent: OfficeIntent) => void;
  // Chat only: the open chat's mode and how to change it
  chatMode?: ChatMode;
  onChatMode?: (m: ChatMode) => void;
}

const MODE_ICON: Record<ChatMode, Parameters<typeof Icon>[0]["name"]> = {
  orchestra: "agents", teamwork: "teamwork", terminal: "terminal",
};


const MODES_MENU_W = 300;

export function ago(iso: string | undefined, lang: string): string {
  if (!iso) return "";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (!Number.isFinite(s) || s < 0) return "";
  const tr = lang === "tr";
  if (s < 60) return tr ? "şimdi" : "now";
  if (s < 3600) return `${Math.floor(s / 60)}${tr ? " dk" : "m"}`;
  if (s < 86400) return `${Math.floor(s / 3600)}${tr ? " sa" : "h"}`;
  return `${Math.floor(s / 86400)}${tr ? " g" : "d"}`;
}

// SessionSidebar is the left rail of a space. In Office it lists agents
// picked from the fixed catalog, each row being the conversation with that
// agent; in Chat it lists sessions by topic, most recent first, and its
// footer switches the open chat's mode (orchestra, teamwork, terminal).
export function SessionSidebar({
  space, workspace, onSelectWorkspace, activeSessionId, onSelectSession, draft, draftAgent, onNewChat,
  rpcOk, dark, onToggleTheme, onOpenMarket, onOpenSettings, onOfficeAgent, chatMode = "orchestra", onChatMode, onBoard,
}: Props) {
  const { lang } = usePrefs();
  const { workspaces, reload: reloadWs } = useWorkspaces();
  const { badgeFor } = usePersonaBadges();
  const { profiles } = useProfiles();
  const [allSessions, setSessions] = useState<Session[]>([]);
  const [modesOpen, setModesOpen] = useState(false);
  // the modes menu floats over the page (the sidebar clips its children),
  // anchored above its button and kept on screen
  const modesMenuRef = useRef<HTMLDivElement | null>(null);
  const [modesAt, setModesAt] = useState<{ left: number; bottom: number } | null>(null);
  const toggleModes = (btn: HTMLElement) => {
    if (modesOpen) { setModesOpen(false); return; }
    const r = btn.getBoundingClientRect();
    const width = MODES_MENU_W;
    setModesAt({
      left: Math.max(8, Math.min(r.left - 8, window.innerWidth - width - 8)),
      bottom: Math.max(8, window.innerHeight - r.top + 8),
    });
    setModesOpen(true);
  };
  const modesRef = useRef<HTMLDivElement | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [filter, setFilter] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  // Code: a new session asks which mode it is for
  const [newOpen, setNewOpen] = useState(false);
  const newRef = useRef<HTMLDivElement | null>(null);
  const [editing, setEditing] = useState<{ id: string; title: string } | null>(null);
  // Per-session job status, for every session regardless of which one is
  // open: green while it is actively working, red once it finishes and the
  // user has not opened it since.
  const [running, setRunning] = useState<Set<string>>(new Set());
  const [doneUnseen, setDoneUnseen] = useState<Set<string>>(new Set());
  const [usage, setUsage] = useState<UsageSnapshot | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const activeIdRef = useRef(activeSessionId);
  useEffect(() => { activeIdRef.current = activeSessionId; }, [activeSessionId]);

  // this space's chats only
  const sessions = useMemo(() => allSessions.filter((s) => spaceOf(s, badgeFor(s.id)) === space), [allSessions, badgeFor, space]);

  const reload = useCallback(async () => {
    try {
      setSessions((await rpc<Session[]>("session.list", { workspaceId: "" })) ?? []);
    } catch { /* keep */ } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    const stop = pollWhileVisible(() => void reload(), 12000);
    const on = () => void reload();
    window.addEventListener("rove:sessions", on);
    // a chat is named after its first message the moment it is sent
    const off = subscribeEvents("session.updated", on);
    return () => { stop(); window.removeEventListener("rove:sessions", on); off(); };
  }, [reload]);

  useEffect(() => {
    return pollWhileVisible(() => { rpc<UsageSnapshot>("usage.get").then(setUsage).catch(() => {}); }, 30000);
  }, []);

  // the office's old picks become agents, once
  useEffect(() => {
    if (space !== "office") return;
    void moveOffice().then((moved) => { if (moved) void reload(); });
  }, [space, reload]);

  // Job status for every session, wherever the run came from (this pane,
  // another pane, or a context-map relay): running while it is active,
  // done-unseen from the moment it finishes until the user opens it.
  useEffect(() => {
    const sessionOf = (ev: { payload?: unknown }) => (ev.payload as { sessionId?: string } | undefined)?.sessionId;
    const markRunning = (ev: { payload?: unknown }) => {
      const sid = sessionOf(ev);
      if (!sid) return;
      setRunning((s) => (s.has(sid) ? s : new Set(s).add(sid)));
      setDoneUnseen((s) => {
        if (!s.has(sid)) return s;
        const next = new Set(s);
        next.delete(sid);
        return next;
      });
    };
    const markDone = (ev: { payload?: unknown }) => {
      const sid = sessionOf(ev);
      if (!sid) return;
      setRunning((s) => {
        if (!s.has(sid)) return s;
        const next = new Set(s);
        next.delete(sid);
        return next;
      });
      if (sid === activeIdRef.current) return; // already looking at it
      setDoneUnseen((s) => (s.has(sid) ? s : new Set(s).add(sid)));
    };
    const offs = [
      subscribeEvents("message.delta", markRunning),
      subscribeEvents("tool.start", markRunning),
      subscribeEvents("tool.result", markRunning),
      subscribeEvents("run.done", markDone),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  // Opening a session — from any path (a row here, a new chat, the context
  // map) — marks it seen.
  useEffect(() => {
    if (!activeSessionId) return;
    setDoneUnseen((s) => {
      if (!s.has(activeSessionId)) return s;
      const next = new Set(s);
      next.delete(activeSessionId);
      return next;
    });
  }, [activeSessionId]);

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false);
      if (newRef.current && !newRef.current.contains(e.target as Node)) setNewOpen(false);
      const target = e.target as Node;
      if (modesRef.current?.contains(target) || modesMenuRef.current?.contains(target)) return;
      setModesOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  const wsById = useMemo(() => new Map(workspaces.map((w) => [w.id, w])), [workspaces]);

  // Code always has something open: the most recent chat, else a new
  // draft. (Agent opens on its board.) Only when nothing is open — a chat
  // that was just created (by a draft's first message) is not in this list
  // until the next reload, and must not be swapped for another one meanwhile.
  useEffect(() => {
    if (space !== "chat" || !loaded || draft || activeSessionId) return;
    if (sessions.length > 0) onSelectSession(sessions[0]);
    else if (space === "chat") onNewChat(null);
  }, [loaded, draft, sessions, activeSessionId, onSelectSession, onNewChat, space]);

  useEffect(() => {
    if (!workspace && workspaces.length > 0) onSelectWorkspace(workspaces[0]);
  }, [workspace, workspaces, onSelectWorkspace]);

  const openFolder = async () => {
    setMenuOpen(false);
    const path = (await pickFolder()).trim();
    if (!path) return;
    const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
    try {
      const ws = await rpc<Workspace>("workspace.open", { path, name });
      await reloadWs();
      onSelectWorkspace(ws);
      onNewChat(null);
    } catch (e) {
      toast(e instanceof Error ? e.message : "open failed", "err");
    }
  };

  const commitRename = async () => {
    if (!editing) return;
    const { id, title } = editing;
    setEditing(null);
    if (!title.trim()) return;
    await rpc("session.rename", { id, title: title.trim() }).catch(() => {});
    await reload();
  };

  const remove = async (s: Session) => {
    await rpc("session.delete", { id: s.id }).catch(() => {});
    if (s.id === activeSessionId) {
      // the open chat is gone: move to the next one, or a new draft
      const next = sessions.find((x) => x.id !== s.id);
      if (next) onSelectSession(next);
      else onNewChat(null);
    }
    await reload();
    window.dispatchEvent(new Event("rove:sessions"));
  };

  const q = filter.trim().toLocaleLowerCase("tr");
  const list = q
    ? sessions.filter((s) => [s.title, wsById.get(s.workspaceId)?.name ?? ""].some((x) => x.toLocaleLowerCase("tr").includes(q)))
    : sessions;
  // Office: the office's agents, each row being its latest conversation
  const byAgent = chatsByProfile(sessions, badgeFor);
  const openAgent = (id: string) => {
    const latest = byAgent.get(id)?.[0];
    if (latest) onSelectSession(latest);
    else onNewChat(id);
  };
  const activeAgent = profiles.find((p) => (byAgent.get(p.id) ?? []).some((x) => x.id === activeSessionId) || (draft && draftAgent === p.id)) ?? null;

  return (
    <aside className={`shell-side panel space-${space}`}>
      {/* the marketplace is a place you go, not a setting: it sits at the
          top of the list rather than among the icons at the foot */}
      <button type="button" className="side-link" onClick={onOpenMarket}>
        <Icon name="cart" size={14} />
        <span>{t("marketplace", lang)}</span>
      </button>
      <div className="side-head">
        <span className="side-title">{t(space === "office" ? "sideAgents" : "sessions", lang)}</span>
        {space === "chat" ? (
          <div className="side-new" ref={menuRef}>
            <button type="button" className="tree-icon" title={t("openWorkspace", lang)} onClick={() => setMenuOpen((v) => !v)}>
              <Icon name="folder" size={14} />
            </button>
            <div className="side-new-mode" ref={newRef}>
              <button type="button" className={`tree-icon${newOpen ? " on" : ""}`} title={t("newSession", lang)} aria-haspopup="menu" aria-expanded={newOpen} onClick={() => setNewOpen((v) => !v)}>
                <Icon name="plus" size={14} />
              </button>
              {newOpen && (
                <div className="pane-menu side-menu modes-menu new-mode-menu" role="menu" aria-label={t("newSessionMode", lang)}>
                  <div className="modes-head">{t("newSessionMode", lang)}</div>
                  {CHAT_MODES.map((m) => (
                    <button type="button" role="menuitem" key={m} onClick={() => { setNewOpen(false); onNewChat(null, m); }}>
                      <Icon name={MODE_ICON[m]} size={14} />
                      <span className="modes-text">
                        <strong>{t(`mode_${m}`, lang)}</strong>
                        <span>{t(`modeHint_${m}`, lang)}</span>
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>
            {menuOpen && (
              <div className="pane-menu side-menu">
                {workspaces.map((ws) => (
                  <button type="button" key={ws.id} className={ws.id === workspace?.id ? "on" : ""} onClick={() => { setMenuOpen(false); onSelectWorkspace(ws); onNewChat(null); }} title={ws.path}>
                    + {t("newSession", lang)} <span className="term-dim">· {ws.name}</span>
                  </button>
                ))}
                <button type="button" onClick={() => void openFolder()}><Icon name="folder" size={13} /> {t("openWorkspace", lang)}…</button>
              </div>
            )}
          </div>
        ) : (
          <button type="button" className="tree-icon" title={t("ofAddInSettings", lang)} onClick={() => onOfficeAgent?.({ mode: "new" })}>
            <Icon name="plus" size={14} />
          </button>
        )}
      </div>

      {space === "chat" ? (
        <>
          {sessions.length > 6 && (
            <div className="side-search">
              <input value={filter} placeholder={t("ctxSearch", lang)} onChange={(e) => setFilter(e.target.value)} />
            </div>
          )}
          <div className="tree">
            {list.map((s) => {
              const live = running.has(s.id);
              const job = live ? "running" : doneUnseen.has(s.id) ? "done" : null;
              const ws = wsById.get(s.workspaceId);
              const active = s.id === activeSessionId;
              return (
                <div key={s.id} className={`sess-row${active ? " active" : ""}`}>
                  {editing?.id === s.id ? (
                    <input
                      autoFocus
                      className="tree-rename"
                      value={editing.title}
                      onChange={(e) => setEditing({ id: s.id, title: e.target.value })}
                      onBlur={() => void commitRename()}
                      onKeyDown={(e) => { if (e.key === "Enter") void commitRename(); if (e.key === "Escape") setEditing(null); }}
                    />
                  ) : (
                    <button
                      type="button"
                      className="sess-main"
                      onClick={() => onSelectSession(s)}
                      onDoubleClick={() => setEditing({ id: s.id, title: s.title })}
                      title={s.title}
                    >
                      <span className="sess-line">
                        <span className={`tree-dot${live ? " live" : active ? " on" : ""}`} />
                        <span className="sess-title">{s.title || t("chat", lang)}</span>
                        <span className="sess-ago">{ago(s.updatedAt, lang)}</span>
                      </span>
                      {/* the root folder ("/") names nothing: only a real project shows */}
                      {ws?.name && ws.name !== "/" && <span className="sess-ws">{ws.name}</span>}
                    </button>
                  )}
                  {job && <span className={`sess-job ${job}`} title={t(job === "running" ? "jobRunning" : "jobDone", lang)} />}
                  <button type="button" className="tree-icon tree-hover" title={t("delete", lang)} onClick={() => void remove(s)}>×</button>
                </div>
              );
            })}
            {loaded && sessions.length === 0 && !workspace && (
              <button type="button" className="tree-empty" onClick={() => void openFolder()}>+ {t("openWorkspace", lang)}</button>
            )}
          </div>
        </>
      ) : (
        <div className="tree office-tree">
          {onBoard && profiles.length > 0 && (
            <button type="button" className={`sess-row staff-board-row${!activeSessionId && !draft ? " active" : ""}`} onClick={onBoard}>
              <Icon name="agents" size={14} />
              <strong>{t("staffTitle", lang)}</strong>
            </button>
          )}
          {activeAgent && (
            <div className="office-hero">
              <AgentMark mark={activeAgent.mark} color={activeAgent.color} seed={activeAgent.id} character={activeAgent.characterId} size={46} />
              <strong>{activeAgent.name}</strong>
              <span>{activeAgent.model || t("ofModelChat", lang)}</span>
            </div>
          )}
          {profiles.length === 0 && (
            <div className="agent-empty">
              <p>{t("ofSideEmpty", lang)}</p>
              <button type="button" className="primary" onClick={() => onOfficeAgent?.({ mode: "new" })}>+ {t("ofAddInSettings", lang)}</button>
            </div>
          )}
          {profiles.map((p) => {
            const chats = byAgent.get(p.id) ?? [];
            const latest = chats[0];
            const on = activeAgent?.id === p.id;
            const live = chats.some((x) => running.has(x.id));
            const job = live ? "running" : chats.some((x) => doneUnseen.has(x.id)) ? "done" : null;
            return (
              <div key={p.id} className={`sess-row agent-chat-row${on ? " active" : ""}`}>
                <button type="button" className="agent-row" onClick={() => openAgent(p.id)} title={p.name}>
                  <AgentMark mark={p.mark} color={p.color} seed={p.id} character={p.characterId} size={26} status={job} />
                  <span className="agent-row-text">
                    <strong>{p.name}</strong>
                    <span>{latest ? latest.title || t("chat", lang) : t("agentStart", lang)}</span>
                  </span>
                  {latest && <span className="agent-chat-ago">{ago(latest.updatedAt, lang)}</span>}
                </button>
                <button type="button" className="tree-icon tree-hover" title={t("ofEditAgent", lang)} onClick={() => onOfficeAgent?.({ mode: "edit", id: p.id })}>
                  <Icon name="sliders" size={13} />
                </button>
              </div>
            );
          })}
        </div>
      )}

      <div className="side-foot">
        <div className="side-who">
          <div className="side-plan"><span className={`side-dot${rpcOk ? " ok" : ""}`} />{t(rpcOk ? "connected" : "offline", lang)}</div>
          <div className="side-usage">{usage ? `${compact(usage.totalTokens)} token · ${usage.calls} ${t("calls", lang)}` : "—"}</div>
        </div>
        {space === "chat" && onChatMode && (
          <div className="side-modes" ref={modesRef}>
            <button
              type="button"
              className={`tree-icon moded${modesOpen ? " on" : ""}`}
              title={`${t("modesMenu", lang)} · ${t(`mode_${chatMode}`, lang)}`}
              aria-haspopup="menu"
              aria-expanded={modesOpen}
              onClick={(e) => toggleModes(e.currentTarget)}
            >
              <Icon name="modes" size={15} />
            </button>
            {modesOpen && createPortal(
              <div
                className="pane-menu modes-menu"
                role="menu"
                ref={modesMenuRef}
                style={modesAt ? { left: modesAt.left, bottom: modesAt.bottom, width: MODES_MENU_W } : undefined}
              >
                <div className="modes-head">{t("modesMenu", lang)}</div>
                {CHAT_MODES.map((m) => (
                  <button
                    type="button"
                    role="menuitemradio"
                    aria-checked={chatMode === m}
                    key={m}
                    className={chatMode === m ? "on" : ""}
                    onClick={() => { setModesOpen(false); onChatMode(m); }}
                  >
                    <Icon name={MODE_ICON[m]} size={14} />
                    <span className="modes-text">
                      <strong>{t(`mode_${m}`, lang)}</strong>
                      <span>{t(`modeHint_${m}`, lang)}</span>
                    </span>
                    {chatMode === m && <Icon name="check" size={12} />}
                  </button>
                ))}
              </div>,
              document.body,
            )}
          </div>
        )}
        <button type="button" className="tree-icon" title={t(dark ? "themeLight" : "themeDark", lang)} onClick={onToggleTheme}><Icon name={dark ? "sun" : "moon"} size={15} /></button>
        <button type="button" className="tree-icon" title={t("settings", lang)} onClick={onOpenSettings}><Icon name="sliders" size={15} /></button>
      </div>
    </aside>
  );
}
