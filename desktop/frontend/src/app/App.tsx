import { lazy, Suspense, useCallback, useState, useEffect } from "react";
import { rpc, hydrateConnection } from "~/lib/rpc";
import type { Session, TerminalSession, Workspace } from "~/lib/types";
import { Chat } from "./Chat";
import { Icon } from "./Icons";
import { TerminalDeck } from "./TerminalDeck";
import { SessionSidebar } from "./SessionSidebar";
import { TeamBar } from "./TeamBar";
import { StaffBoard } from "./StaffBoard";
import { PanelGuard } from "./PanelGuard";
import { SubagentDock } from "./SubagentDock";
import { PermissionAsk } from "./PermissionAsk";
import type { AutoTab } from "./AutomationPage";
// pages opened now and then load when first opened: the maps, settings and
// the skill market are most of the app's code, and a chat needs none of it
const AutomationPage = lazy(() => import("./AutomationPage").then((m) => ({ default: m.AutomationPage })));
const Settings = lazy(() => import("./Settings").then((m) => ({ default: m.Settings })));
const SkillMarket = lazy(() => import("./SkillMarket").then((m) => ({ default: m.SkillMarket })));
const TeamworkView = lazy(() => import("./TeamworkView").then((m) => ({ default: m.TeamworkView })));
import { Toasts } from "./Toasts";
import { CommandPalette, type PaletteAction } from "./CommandPalette";
import { useWorkspaces } from "~/hooks/useApi";
import { usePersonaBadges } from "~/hooks/usePersona";
import { usePrefs } from "~/hooks/usePrefs";
import { applyTheme, isLightTheme, type ThemeId } from "~/lib/themes";
import { t } from "~/lib/i18n";
import {
  adopt, freshDraft, loadModes, loadTop, modeFor, saveModes, saveTop, spaceOf, TOPS, withMode,
  type ChatMode, type Space, type Top,
} from "~/lib/spaces";
import brandMark from "~/assets/logo-256.png";
import { watchFinishes } from "~/lib/notify";
import { ConnectionBanner, ConnectionMenu } from "./ConnectionMenu";
import { FolderBrowserHost } from "./FolderBrowser";
import { ApiBanner } from "./ApiBanner";
import type { OfficeIntent } from "./OfficePanel";
import { UpdateBanner } from "./UpdateBanner";
import { SIDE, columns, dragWidth, fitsSide, loadWidth, maxWidth } from "~/lib/layout";
import { pollWhileVisible } from "~/lib/poll";

// The app is three spaces, one page each: Office (pick an agent, talk to
// it), Chat (session work; each chat has a mode: orchestra, teamwork or
// terminal) and Automation (Context Map and Session Map).
// Market is a page reached from the sidebar.
const TOP_KEYS: Record<Exclude<Top, "market">, string> = { office: "spaceOffice", chat: "spaceChat", automation: "spaceAutomation" };
const AUTO_TAB_KEY = "aether.auto.tab";

type SettingsSection = "appearance" | "workspace" | "providers" | "goals" | "agents" | "roster" | "permissions" | "automation" | "memory" | "profiles" | "servers" | "usage";

// What a space has open: a chat, a draft (a chat that exists only once its
// first message is sent; in Office, with the agent it will talk to), or
// nothing.
type Sel = { session: Session | null; draft: boolean; agent: string | null };
const NOTHING: Sel = { session: null, draft: false, agent: null };

function readFlag(key: string, fallback: boolean): boolean {
  try {
    const v = localStorage.getItem(key);
    return v == null ? fallback : v === "1";
  } catch {
    return fallback;
  }
}

function writeFlag(key: string, v: boolean) {
  try { localStorage.setItem(key, v ? "1" : "0"); } catch { /* private */ }
}

function loadAutoTab(): AutoTab {
  try {
    const v = localStorage.getItem(AUTO_TAB_KEY);
    return v === "sessions" || v === "code" ? v : "context";
  } catch {
    return "context";
  }
}

export function App() {
  const [ready, setReady] = useState(false);
  const [top, setTopState] = useState<Top>(loadTop);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [sel, setSel] = useState<Record<Space, Sel>>({ office: NOTHING, chat: NOTHING });
  // Orchestra: which channel of the Chat space's chat is open — null is the
  // chat itself, otherwise a subagent's channel opened from the dock.
  const [channel, setChannel] = useState<Session | null>(null);
  // each Chat-space chat keeps the mode it was last used in
  const [modes, setModesState] = useState<Record<string, ChatMode>>(loadModes);
  const [autoTab, setAutoTabState] = useState<AutoTab>(loadAutoTab);
  const { badgeFor } = usePersonaBadges();
  // A shell opened from Settings → Servers, waiting to land in a
  // TerminalDeck pane once the Chat space shows its terminal mode.
  const [pendingShell, setPendingShell] = useState<TerminalSession | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState<SettingsSection>("appearance");
  const [officeIntent, setOfficeIntent] = useState<OfficeIntent>({ mode: "list" });
  const [paletteOpen, setPaletteOpen] = useState(false);
  const { lang, theme } = usePrefs();
  const { workspaces } = useWorkspaces();
  const [rpcOk, setRpcOk] = useState(true);
  const [sideOpen, setSideOpen] = useState(() => readFlag("aether.shell.side", true));
  const [sideW, setSideW] = useState(() => loadWidth("aether.shell.sideW", SIDE.def, SIDE.min, SIDE.max));
  const [dragging, setDragging] = useState(false);
  const [winW, setWinW] = useState(() => window.innerWidth);

  // desktop notifications for work that finishes while the app is away
  useEffect(() => watchFinishes(), []);

  useEffect(() => {
    const on = () => setWinW(window.innerWidth);
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, []);

  const space: Space | null = top === "office" || top === "chat" ? top : null;
  const chat = sel.chat;
  const chatMode = modeFor(modes, chat.session?.id);
  // the sidebar belongs to Office and Chat; Automation and Market are full pages
  const showSide = sideOpen && space !== null;
  // too narrow for two columns: the list still opens, but over the page
  const sideOver = showSide && !fitsSide(winW);
  // Shrinking the window can push a saved width past its current ceiling
  // (see lib/layout maxWidth); ease it back down instead of overflowing. A
  // live drag already stays in bounds via dragWidth.
  useEffect(() => {
    setSideW((w) => Math.min(w, maxWidth("left", 0, winW)));
  }, [winW, showSide]);

  const setTop = useCallback((next: Top) => {
    setTopState(next);
    saveTop(next);
  }, []);
  const setModes = useCallback((next: Record<string, ChatMode>) => {
    setModesState(next);
    saveModes(next);
  }, []);
  const setChatMode = useCallback((m: ChatMode) => {
    setModesState((cur) => {
      const next = withMode(cur, sel.chat.session?.id, m);
      saveModes(next);
      return next;
    });
    if (m !== "orchestra") setChannel(null);
  }, [sel.chat.session]);
  const setAutoTab = useCallback((next: AutoTab) => {
    setAutoTabState(next);
    try { localStorage.setItem(AUTO_TAB_KEY, next); } catch { /* private */ }
  }, []);

  // Selecting a chat also moves the workspace to the chat's own, so the
  // file tree and new chats follow it.
  const followWorkspace = useCallback((s: Session) => {
    if (s.workspaceId && s.workspaceId !== workspace?.id) {
      const ws = workspaces.find((w) => w.id === s.workspaceId);
      if (ws) setWorkspace(ws);
    }
  }, [workspace, workspaces]);

  const selectIn = useCallback((sp: Space, s: Session) => {
    setSel((cur) => ({ ...cur, [sp]: { session: s, draft: false, agent: null } }));
    if (sp === "chat") setChannel((c) => (c && c.parentId === s.id ? c : null));
    followWorkspace(s);
  }, [followWorkspace]);

  // openSession opens a chat from anywhere (a map, a terminal pane) in the
  // space it belongs to.
  const openSession = useCallback((s: Session) => {
    const sp = spaceOf(s, badgeFor(s.id));
    selectIn(sp, s);
    setTop(sp);
  }, [badgeFor, selectIn, setTop]);

  // openSessionById opens a chat known only by its id (a task's, from the
  // Agent space's board).
  const openSessionById = useCallback(async (id: string) => {
    const list = (await rpc<Session[]>("session.list", { workspaceId: "" }).catch(() => [])) ?? [];
    const s = list.find((x) => x.id === id);
    if (s) openSession(s);
  }, [openSession]);

  // The Agent space's board: no chat open.
  const showBoard = useCallback(() => {
    setSel((cur) => ({ ...cur, office: NOTHING }));
    setTop("office");
  }, [setTop]);

  // A new chat in a space: a draft (in Office, with the agent it will talk
  // to; in Chat, in the mode picked for it, else the one used last). Office
  // without an agent has nothing to open.
  const newDraft = useCallback((sp: Space, agent: string | null = null, mode?: ChatMode) => {
    const next: Sel = sp === "office" && !agent ? NOTHING : { session: null, draft: true, agent };
    setSel((cur) => ({ ...cur, [sp]: next }));
    if (sp === "chat") {
      setChannel(null);
      // a new chat starts in the mode picked for it, else the one used last
      setModesState((cur) => {
        const m = mode ? withMode(freshDraft(cur), null, mode) : freshDraft(cur);
        saveModes(m);
        return m;
      });
    }
    setTop(sp);
  }, [setTop]);

  // The draft's first message creates the chat, in its space and (in
  // Office) already talking to its agent, in one call. The daemon names it
  // after that message.
  const createFromDraft = useCallback(async (sp: Space): Promise<Session> => {
    const agent = sel[sp].agent;
    const s = await rpc<Session>("session.create", {
      title: "",
      workspaceId: workspace?.id ?? "",
      space: sp,
      // an office chat talks to one of the office's agents (a profile)
      profileIds: sp === "office" && agent ? [agent] : [],
    });
    setSel((cur) => ({ ...cur, [sp]: { session: s, draft: false, agent: null } }));
    if (sp === "chat") {
      setChannel(null);
      setModes(adopt(modes, s.id));
    }
    window.dispatchEvent(new Event("rove:sessions"));
    window.dispatchEvent(new Event("rove:persona"));
    return s;
  }, [sel, workspace, modes, setModes]);

  const toggleSide = useCallback(() => setSideOpen((v) => { writeFlag("aether.shell.side", !v); return !v; }), []);
  const openSettings = useCallback((section: SettingsSection = "appearance") => {
    setSettingsSection(section);
    setOfficeIntent({ mode: "list" });
    setSettingsOpen(true);
  }, []);
  // the office is staffed in Settings → Office: a new agent, or one to edit
  const openOffice = useCallback((intent: OfficeIntent) => {
    setSettingsSection("profiles");
    setOfficeIntent(intent);
    setSettingsOpen(true);
  }, []);
  // Market has no tab of its own — remember what was showing before it so
  // the back button can return there.
  const [beforeMarket, setBeforeMarket] = useState<Exclude<Top, "market">>("office");
  const openMarket = useCallback(() => {
    if (top !== "market") setBeforeMarket(top);
    setTop("market");
  }, [top, setTop]);
  const openMap = useCallback((tab: AutoTab) => { setAutoTab(tab); setTop("automation"); }, [setAutoTab, setTop]);

  useEffect(() => {
    hydrateConnection()
      .then(() => { setReady(true); setRpcOk(true); })
      .catch(() => { setReady(true); setRpcOk(false); });
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const meta = e.metaKey || e.ctrlKey;
      if (meta && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        setPaletteOpen((v) => !v);
        return;
      }
      // ⌘1…⌘3 switch spaces
      if (meta && /^[1-3]$/.test(e.key)) {
        e.preventDefault();
        setTop(TOPS[Number(e.key) - 1]);
        return;
      }
      if (e.key !== "Escape") return;
      if (paletteOpen) setPaletteOpen(false);
      else if (settingsOpen) setSettingsOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [settingsOpen, paletteOpen, setTop]);

  useEffect(() => {
    if (!ready) return;
    return pollWhileVisible(() => { rpc("ping").then(() => setRpcOk(true)).catch(() => setRpcOk(false)); }, 8000);
  }, [ready]);

  const dark = !isLightTheme(theme as ThemeId);
  const toggleTheme = useCallback(() => applyTheme(dark ? "daylight" : "graphite"), [dark]);

  if (!ready) {
    return <div className="boot">Starting Rove Code…</div>;
  }

  // Drag the gutter to resize the sidebar; double-click resets it.
  const startDrag = (e: React.PointerEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const start = sideW;
    let last = start;
    setDragging(true);
    const move = (ev: PointerEvent) => {
      last = dragWidth("left", start, ev.clientX - startX, 0, window.innerWidth);
      setSideW(last);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      setDragging(false);
      try { localStorage.setItem("aether.shell.sideW", String(last)); } catch { /* private */ }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const resetWidth = () => {
    setSideW(SIDE.def);
    try { localStorage.removeItem("aether.shell.sideW"); } catch { /* private */ }
  };

  // A conversation in a space. `session` may be a subagent's channel (Orchestra).
  const conversation = (sp: Space, session: Session | null, team?: React.ReactNode) => (
    <Chat
      // the subagents belong to the chat, whichever of its channels is open
      dock={<SubagentDock session={sp === "chat" ? chat.session : session} lang={lang} onOpen={(c) => { if (sp === "chat") setChannel(c); else selectIn(sp, c); }} />}
      key={sp}
      workspaceId={workspace?.id}
      workspacePath={workspace?.path}
      session={session}
      draft={sel[sp].draft && !sel[sp].session}
      onCreateSession={() => createFromDraft(sp)}
      // /new keeps talking to the same agent
      onNewDraft={() => newDraft(sp, sp === "office" ? (sel.office.session ? badgeFor(sel.office.session.id)?.profileId ?? null : sel.office.agent) : null)}
      onSession={(s) => { selectIn(sp, s); window.dispatchEvent(new Event("rove:sessions")); }}
      onOpenMap={() => openMap("context")}
      onOpenContext={() => openMap("sessions")}
      team={team}
    />
  );

  function renderCenter() {
    switch (top) {
      case "office": {
        const o = sel.office;
        // nothing open: the team itself, who is working on what
        if (!o.session && !o.draft) {
          return (
            <div className="panel center-panel">
              <StaffBoard
                workspace={workspace}
                onOpenSession={(id) => void openSessionById(id)}
                onTalk={(id) => newDraft("office", id)}
                onEdit={(id) => openOffice({ mode: "edit", id })}
                onAdd={() => openOffice({ mode: "new" })}
              />
            </div>
          );
        }
        return <div className="panel center-panel">{conversation("office", o.session)}</div>;
      }
      case "automation":
        return (
          <Suspense fallback={<div className="panel center-panel" />}>
            <AutomationPage
              tab={autoTab}
              onTab={setAutoTab}
              workspace={workspace}
              sessionId={chat.session?.id}
              onOpenSession={openSession}
            />
          </Suspense>
        );
      case "market":
        return (
          <div className="panel center-panel">
            <div className="market-head">
              <button type="button" className="market-back" onClick={() => setTop(beforeMarket)}>
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M15 5l-7 7 7 7" /></svg>
                {t("back", lang)}
              </button>
            </div>
            <Suspense fallback={null}><SkillMarket /></Suspense>
          </div>
        );
      default:
        switch (chatMode) {
          case "terminal":
            return (
              <TerminalDeck
                activeSession={chat.session}
                draft={chat.draft && !chat.session}
                onCreateSession={() => createFromDraft("chat")}
                workspaceId={workspace?.id}
                // a chat focused in a pane is being used in the terminal
                onFocusSession={(s) => { selectIn("chat", s); setModesState((cur) => { const next = withMode(cur, s.id, "terminal"); saveModes(next); return next; }); }}
                onOpenMap={() => openMap("context")}
                onOpenContext={() => openMap("sessions")}
                onNewSession={() => newDraft("chat")}
                incomingShell={pendingShell}
                onConsumedShell={() => setPendingShell(null)}
                themeKey={theme}
              />
            );
          case "teamwork":
            return (
              <div className="panel center-panel flush">
                <Suspense fallback={null}>
                  <TeamworkView
                    session={chat.session}
                    draft={chat.draft && !chat.session}
                    onCreateSession={() => createFromDraft("chat")}
                    workspace={workspace}
                  />
                </Suspense>
              </div>
            );
          default: // orchestra: the conversation; a subagent's channel opens over it
            return (
              <div className="panel center-panel">
                {conversation("chat", channel ?? chat.session, channel ? <TeamBar channel={channel} onClose={() => setChannel(null)} /> : undefined)}
              </div>
            );
        }
    }
  }

  return (
    <div className="shell">
      <header className="titlebar2">
        <div className="tb-left">
          <span className="tb-brand">
            <img src={brandMark} alt="" />
            Rove
          </span>
          <button type="button" className={`tb-icon${showSide ? "" : " off"}`} title={t("toggleSide", lang)} onClick={toggleSide} disabled={space === null}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"><rect x="3" y="4" width="18" height="16" rx="3" /><path d="M9 4v16" /></svg>
          </button>
        </div>
        <nav className="tb-modes" role="tablist">
          {TOPS.map((m) => (
            <button
              key={m}
              type="button"
              role="tab"
              aria-selected={top === m}
              className={top === m ? "on" : ""}
              onClick={() => setTop(m)}
            >
              {t(TOP_KEYS[m], lang)}
            </button>
          ))}
        </nav>
        <div className="tb-right">
          <button type="button" className="tb-icon" title="⌘K" onClick={() => setPaletteOpen(true)}><Icon name="search" size={15} /></button>
          <span className={`tb-status${rpcOk ? " ok" : ""}`} title={rpcOk ? t("connected", lang) : "offline"} />
          <ConnectionMenu lang={lang} onAddServer={() => openSettings("servers")} />
          <button type="button" className="tb-icon" title={t("settings", lang)} onClick={() => openSettings("appearance")}><Icon name="sliders" size={15} /></button>
        </div>
      </header>

      <div
        className={`shell-body${showSide ? "" : " no-side"}${sideOver ? " side-over" : ""}${dragging ? " resizing" : ""}`}
        style={{ gridTemplateColumns: columns(showSide && !sideOver ? sideW : null, null) }}
      >
        {sideOver && (
          <button type="button" className="side-scrim" aria-label={t("close", lang)} onClick={toggleSide} />
        )}
        {showSide && space && (
          <SessionSidebar
            key={space}
            space={space}
            workspace={workspace}
            onSelectWorkspace={setWorkspace}
            activeSessionId={sel[space].session?.id ?? null}
            onSelectSession={(s) => selectIn(space, s)}
            draft={sel[space].draft && !sel[space].session}
            draftAgent={sel[space].agent}
            onNewChat={(agent, mode) => newDraft(space, agent ?? null, mode)}
            onBoard={showBoard}
            rpcOk={rpcOk}
            dark={dark}
            onToggleTheme={toggleTheme}
            onOpenMarket={openMarket}
            onOpenSettings={() => openSettings("appearance")}
            onOfficeAgent={openOffice}
            chatMode={chatMode}
            onChatMode={setChatMode}
          />
        )}
        {showSide && !sideOver && (
          <div
            className={`gutter${dragging ? " active" : ""}`}
            role="separator"
            aria-orientation="vertical"
            title={t("resizeHint", lang)}
            onPointerDown={startDrag}
            onDoubleClick={resetWidth}
          />
        )}
        <main className="shell-main"><PanelGuard key={top}>{renderCenter()}</PanelGuard></main>
      </div>


      {settingsOpen && (
        <Suspense fallback={null}>
          <Settings
            onClose={() => setSettingsOpen(false)}
            onSelectWorkspace={setWorkspace}
            onOpenShell={(sess) => { setPendingShell(sess); setTop("chat"); setChatMode("terminal"); }}
            initialSection={settingsSection}
            officeIntent={officeIntent}
            workspace={workspace}
            sessionId={chat.session?.id}
          />
        </Suspense>
      )}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        onOpenSession={openSession}
        actions={[
          ...TOPS.map((m) => ({ id: m, label: t(TOP_KEYS[m], lang), run: () => setTop(m) })),
          { id: "market", label: t("marketplace", lang), run: openMarket },
          { id: "settings", label: t("settings", lang), run: () => openSettings("appearance") },
          { id: "profiles", label: t("personaProfile", lang), run: () => openSettings("profiles") },
          { id: "memory", label: t("memory", lang), run: () => openSettings("memory") },
          { id: "new", label: t("slashNew", lang), run: () => newDraft("chat") },
        ] as PaletteAction[]}
      />

      <PermissionAsk />
      <Toasts />
      <ConnectionBanner lang={lang} />
      <ApiBanner lang={lang} />
      <FolderBrowserHost lang={lang} />

      <UpdateBanner lang={lang} />
    </div>
  );
}
