import { useCallback, useState, useEffect, useRef } from "react";
import { rpc, hydrateConnection } from "~/lib/rpc";
import type { Card, Session, TerminalSession, Workspace } from "~/lib/types";
import { Chat } from "./Chat";
import { Kanban } from "./Kanban";
import { Terminal } from "./Terminal";
import { Settings } from "./Settings";
import { WorkspacePanel, SessionPanel } from "./Workspace";
import { FileTree } from "./FileTree";
import { SessionRail } from "./SessionRail";
import { CheckpointPanel } from "./CheckpointPanel";
import { SkillMarket } from "./SkillMarket";
import { CardDetail } from "./CardDetail";
import { Icon } from "./Icons";
import { Extensions } from "./Extensions";
import { GeneralPanel } from "./GeneralPanel";
import { UsageCard } from "./UsageCard";
import { CreateAgentPopover, ConnectSSHPopover, loadSSHHosts } from "./Popovers";
import { Toasts } from "./Toasts";
import { CommandPalette, type PaletteAction } from "./CommandPalette";
import { useAgents } from "~/hooks/useApi";
import type { SSHTarget } from "~/lib/types";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import brandMark from "~/assets/logo-256.png";

type WinKind = "chat" | "board" | "terminal" | "market";
type Win = { id: string; kind: WinKind; title: string; termId?: string };

export function App() {
  const [ready, setReady] = useState(false);
  const [windows, setWindows] = useState<Win[]>(() => [
    { id: "chat", kind: "chat", title: t("chat") },
    { id: "board", kind: "board", title: t("board") },
  ]);
  const [activeWin, setActiveWin] = useState("chat");
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [termSessions, setTermSessions] = useState<TerminalSession[]>([]);
  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [activeSession, setActiveSession] = useState<Session | null>(null);
  const [agentActivity, setAgentActivity] = useState<Record<string, string>>({});
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState<"appearance" | "workspace" | "providers" | "goals" | "agents" | "roster" | "permissions" | "automation" | "memory">("appearance");
  const [paletteOpen, setPaletteOpen] = useState(false);
  const { lang, theme } = usePrefs();
  const [createAgentOpen, setCreateAgentOpen] = useState(false);
  const [connectOpen, setConnectOpen] = useState(false);
  const [sshHosts, setSshHosts] = useState<SSHTarget[]>(() => loadSSHHosts());
  const { agents, reload: reloadAgents } = useAgents();
  const newChatRef = useRef<(() => Promise<Session | null>) | null>(null);
  const [rpcOk, setRpcOk] = useState(true);
  const [updateAvail, setUpdateAvail] = useState<string | null>(null);
  const [auxOpen, setAuxOpen] = useState(() => localStorage.getItem("aether.auxOpen") !== "0");
  const [sideOpen, setSideOpen] = useState(() => localStorage.getItem("aether.sideOpen") === "1");

  const toggleAux = useCallback(() => {
    setAuxOpen((v) => {
      const next = !v;
      localStorage.setItem("aether.auxOpen", next ? "1" : "0");
      return next;
    });
  }, []);

  const toggleSide = useCallback(() => {
    setSideOpen((v) => {
      const next = !v;
      localStorage.setItem("aether.sideOpen", next ? "1" : "0");
      return next;
    });
  }, []);


  const openNewChat = useCallback(async () => {
    const sess = await newChatRef.current?.();
    if (sess) setActiveSession(sess);
    setActiveWin("chat");
  }, []);

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
      if (e.key !== "Escape") return;
      if (paletteOpen) setPaletteOpen(false);
      else if (settingsOpen) setSettingsOpen(false);
      else if (createAgentOpen) setCreateAgentOpen(false);
      else if (connectOpen) setConnectOpen(false);
      else if (selectedCard) setSelectedCard(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [settingsOpen, createAgentOpen, connectOpen, selectedCard, paletteOpen]);

  useEffect(() => {
    setWindows((w) =>
      w.map((x) =>
        x.id === "chat" ? { ...x, title: t("chat", lang) } :
        x.id === "board" ? { ...x, title: t("board", lang) } :
        x.id === "market" ? { ...x, title: t("marketplace", lang) } :
        x,
      ),
    );
  }, [lang]);

  const spawnTerm = useCallback(async (focus = true) => {
    try {
      const sess = await rpc<TerminalSession>("terminal.spawn", {
        kind: "user",
        cwd: workspace?.path ?? "",
      });
      setTermSessions((s) => [...s, sess]);
      const win: Win = {
        id: `term-${sess.id}`,
        kind: "terminal",
        title: sess.title || "shell",
        termId: sess.id,
      };
      setWindows((w) => (w.some((x) => x.id === win.id) ? w : [...w, win]));
      if (focus) setActiveWin(win.id);
      return sess;
    } catch {
      return null;
    }
  }, [workspace]);

  const attachSSH = useCallback((sess: TerminalSession) => {
    setTermSessions((s) => (s.some((t) => t.id === sess.id) ? s : [...s, sess]));
    const win: Win = {
      id: `term-${sess.id}`,
      kind: "terminal",
      title: sess.title || sess.ssh?.host || "ssh",
      termId: sess.id,
    };
    setWindows((w) => (w.some((x) => x.id === win.id) ? w : [...w, win]));
    setActiveWin(win.id);
    setSshHosts(loadSSHHosts());
  }, []);

  const reopenSSH = useCallback(async (target: SSHTarget) => {
    try {
      const sess = await rpc<TerminalSession>("ssh.open", target);
      attachSSH(sess);
    } catch {
      setConnectOpen(true);
    }
  }, [attachSSH]);

  useEffect(() => {
    if (!ready) return;
    const tick = () => {
      rpc("ping").then(() => setRpcOk(true)).catch(() => setRpcOk(false));
    };
    tick();
    const id = window.setInterval(tick, 8000);
    return () => window.clearInterval(id);
  }, [ready]);

  useEffect(() => {
    if (!ready) return;
    const CURRENT = "v0.1.0";
    const check = async () => {
      try {
        const r = await fetch(
          "https://api.github.com/repos/Kayra-ML/Rove_cli/releases/latest",
          { headers: { Accept: "application/vnd.github+json" } }
        );
        if (!r.ok) return;
        const j = (await r.json()) as { tag_name?: string };
        const tag = j.tag_name ?? "";
        if (tag && tag !== CURRENT) setUpdateAvail(tag);
      } catch { /* offline */ }
    };
    void check();
    const id = window.setInterval(check, 6 * 60 * 60 * 1000); // every 6h
    return () => window.clearInterval(id);
  }, [ready]);

  const closeWin = useCallback((id: string) => {
    const win = windows.find((x) => x.id === id);
    if (win?.kind === "terminal" && win.termId) {
      void rpc("terminal.kill", { id: win.termId }).catch(() => {});
      setTermSessions((s) => s.filter((t) => t.id !== win.termId));
    }
    setWindows((w) => {
      const next = w.filter((x) => x.id !== id);
      setActiveWin((cur) => (cur === id ? (next[0]?.id ?? "chat") : cur));
      return next;
    });
  }, [windows]);

  const openMarket = useCallback(() => {
    setWindows((w) => (w.some((x) => x.id === "market") ? w : [...w, { id: "market", kind: "market", title: t("marketplace", lang) }]));
    setActiveWin("market");
  }, [lang]);

  if (!ready) {
    return (
      <div style={{ height: "100%", display: "flex", alignItems: "center", justifyContent: "center", color: "var(--muted)" }}>
        Starting Rove Code…
      </div>
    );
  }

  const current = windows.find((w) => w.id === activeWin) ?? windows[0];

  function renderMain() {
    if (!current) return null;
    if (current.kind === "market") {
      return (
        <div className={`body body-market${sideOpen ? "" : " body-side-off"}`}>
          <div className="sidebar" hidden={!sideOpen}>
            <div className="aux-head">
              <span>{t("sessions", lang)}</span>
              <button type="button" className="icon-btn" title="‹" onClick={toggleSide}>‹</button>
            </div>
            <div className="sidebar-top">
              <Extensions onOpenMarket={openMarket} marketActive />
              <WorkspacePanel active={workspace} onSelect={setWorkspace} onNewChat={() => void openNewChat()} />
              <SessionPanel
                workspaceId={workspace?.id}
                activeId={activeSession?.id ?? null}
                onSelect={setActiveSession}
                createRef={newChatRef}
              />
            </div>
            <UsageCard />
            <GeneralPanel />
          </div>
          <div className="main catalog-main">
            <SkillMarket />
          </div>
        </div>
      );
    }
    if (current.kind === "terminal") {
      return (
        <Terminal
          sessions={termSessions}
          activeId={current.termId ?? null}
          onSelect={(id) => setActiveWin(`term-${id}`)}
          onSpawn={() => void spawnTerm(true)}
          onRestart={(id) => {
            void rpc<TerminalSession>("terminal.restart", { id }).then((sess) => {
              setTermSessions((s) => s.map((t) => (t.id === id ? { ...t, ...sess, id } : t)));
            }).catch(() => {});
          }}
          onDetach={(id) => {
            void rpc("terminal.detach", { id }).catch(() => {});
          }}
          hideTabs
          themeKey={theme}
        />
      );
    }
    return (
      <div className={`body${auxOpen ? "" : " body-aux-off"}${sideOpen ? "" : " body-side-off"}`}>
        <div className="sidebar" hidden={!sideOpen}>
          <div className="aux-head">
            <span>{t("sessions", lang)}</span>
            <button type="button" className="icon-btn" title="‹" onClick={toggleSide}>‹</button>
          </div>
          <div className="sidebar-top">
            <Extensions onOpenMarket={openMarket} marketActive={false} />
            <WorkspacePanel active={workspace} onSelect={setWorkspace} onNewChat={() => void openNewChat()} />
            <SessionPanel
              workspaceId={workspace?.id}
              activeId={activeSession?.id ?? null}
              onSelect={setActiveSession}
              createRef={newChatRef}
            />
          </div>
          <UsageCard />
          <GeneralPanel />
        </div>
        <div className="main">
          {current.kind === "board" ? (
            <Kanban workspaceId={workspace?.id} sessionId={activeSession?.id} onSelectCard={setSelectedCard} />
          ) : (
            <Chat
              workspaceId={workspace?.id}
              session={activeSession}
              onSession={setActiveSession}
              onActivity={setAgentActivity}
              onOpenBoard={() => setActiveWin("board")}
              onSelectCard={setSelectedCard}
            />
          )}
        </div>
        {auxOpen ? (
        <div className="aux">
          <div className="aux-head">
            <span>{t("plans", lang)}</span>
            <button type="button" className="icon-btn" title="›" onClick={toggleAux}>›</button>
          </div>
          <SessionRail
            session={activeSession}
            workspaceId={workspace?.id}
            workspacePath={workspace?.path}
            activity={agentActivity}
            onSelectCard={setSelectedCard}
            onOpenRoster={() => { setSettingsSection("roster"); setSettingsOpen(true); }}
          />
          {workspace && (
            <>
              <div className="section-label">{t("workspaces", lang)}</div>
              <div style={{ padding: "4px 12px 10px", color: "var(--muted)", fontSize: 12 }}>
                <div style={{ fontWeight: 600, color: "var(--text)" }}>{workspace.name}</div>
                <div style={{ fontSize: 10, marginTop: 2 }}>{workspace.path}</div>
                <div style={{ fontSize: 10, marginTop: 2 }}>branch: {workspace.defaultBranch}</div>
              </div>
              <FileTree root={workspace.path} />
              <CheckpointPanel workspacePath={workspace.path} />
            </>
          )}
          <div className="aux-bottom">
            <div className="section-label section-label-row">
              <span>{t("profiles", lang)}</span>
              <button className="icon-btn" title={t("newProfile", lang)} onClick={() => setCreateAgentOpen(true)}>
                <Icon name="plus" size={15} />
              </button>
            </div>
            {agents.length === 0 ? (
              <div style={{ padding: "4px 14px", color: "var(--faint)", fontSize: 12 }}>{t("noneYet", lang)}</div>
            ) : (
              agents.map((a) => (
                <button
                  key={a.id}
                  className="nav-item"
                  onClick={() => { setSettingsSection("roster"); setSettingsOpen(true); }}
                  title={`${a.provider} / ${a.model}`}
                >
                  <span className={`pip${a.status === "running" ? " run" : " on"}`} style={{ marginRight: 8 }} />
                  {a.name}
                  <span style={{ display: "block", fontSize: 11, color: "var(--muted)", paddingLeft: 15 }}>
                    {a.model}
                  </span>
                </button>
              ))
            )}
            <div className="section-label section-label-row">
              <span>{t("servers", lang)}</span>
              <button className="icon-btn" title={t("connectServer", lang)} onClick={() => setConnectOpen(true)}>
                <Icon name="plus" size={15} />
              </button>
            </div>
            {sshHosts.length === 0 ? (
              <div style={{ padding: "4px 14px", color: "var(--faint)", fontSize: 12 }}>{t("noneYet", lang)}</div>
            ) : (
              sshHosts.map((h) => {
                const live = termSessions.find((s) => s.ssh?.host === h.host && s.status === "running");
                const label = h.user ? `${h.user}@${h.host}` : h.host;
                return (
                  <button
                    key={`${h.user ?? ""}@${h.host}:${h.port ?? 22}`}
                    className="nav-item"
                    onClick={() => {
                      if (live) {
                        setActiveWin(`term-${live.id}`);
                        return;
                      }
                      void reopenSSH(h);
                    }}
                    title={`${h.host}:${h.port ?? 22}`}
                  >
                    <span className={`pip${live ? " on" : ""}`} style={{ marginRight: 8 }} />
                    {label}
                    <span style={{ display: "block", fontSize: 11, color: "var(--muted)", paddingLeft: 15 }}>
                      :{h.port ?? 22} · {h.authMethod || "agent"}
                    </span>
                  </button>
                );
              })
            )}
          </div>
        </div>
        ) : (
          <button type="button" className="aux-show" onClick={toggleAux} title={t("plans", lang)}>‹</button>
        )}
      </div>
    );
  }

  return (
    <div className="app">
      <div className="topbar">
        <span className="brand">
          <img className="brand-mark" src={brandMark} alt="" />
          Rove
        </span>
        <div className="win-tabs">
          {windows.map((w) => (
            <button
              key={w.id}
              className={`win-tab${activeWin === w.id ? " active" : ""}`}
              onClick={() => setActiveWin(w.id)}
              onAuxClick={(e) => {
                if (e.button === 1 && w.kind !== "chat" && w.kind !== "board") {
                  e.preventDefault();
                  closeWin(w.id);
                }
              }}
            >
              {w.kind === "chat" && <Icon name="chat" size={12} />}
              {w.kind === "board" && <Icon name="kanban" size={12} />}
              {w.kind === "terminal" && <Icon name="terminal" size={12} />}
              {w.kind === "market" && <Icon name="cart" size={12} />}
              <span>{w.title}</span>
              {w.kind !== "chat" && w.kind !== "board" && (
                <span
                  className="win-tab-x"
                  onClick={(e) => { e.stopPropagation(); closeWin(w.id); }}
                >
                  ×
                </span>
              )}
            </button>
          ))}
          <button className="win-tab add" title="+" onClick={() => void spawnTerm(true)}>
            +
          </button>
        </div>
        <div className="spacer" />
        <div className="icon-nav">
          <button
            type="button"
            className={`icon-btn${!sideOpen ? " active" : ""}`}
            title={t("sessions", lang)}
            onClick={toggleSide}
          >
            {sideOpen ? "‹" : "›"}
          </button>
          <button
            type="button"
            className={`icon-btn${!auxOpen ? " active" : ""}`}
            title={t("plans", lang)}
            onClick={toggleAux}
          >
            {auxOpen ? "›" : "‹"}
          </button>
          <button
            className={`icon-btn${settingsOpen ? " active" : ""}`}
            title={t("settings", lang)}
            onClick={() => { setSettingsSection("appearance"); setSettingsOpen(true); }}
          >
            <Icon name="sliders" size={18} />
          </button>
        </div>
      </div>

      {renderMain()}

      {selectedCard && (
        <CardDetail card={selectedCard} onClose={() => setSelectedCard(null)} />
      )}

      {settingsOpen && (
        <Settings
          onClose={() => setSettingsOpen(false)}
          onSelectWorkspace={setWorkspace}
          initialSection={settingsSection}
          workspace={workspace}
          sessionId={activeSession?.id}
        />
      )}
      {createAgentOpen && (
        <CreateAgentPopover onClose={() => { setCreateAgentOpen(false); void reloadAgents(); }} />
      )}
      {connectOpen && (
        <ConnectSSHPopover
          onClose={() => setConnectOpen(false)}
          onOpened={attachSSH}
        />
      )}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        actions={[
          { id: "chat", label: t("chat", lang), run: () => setActiveWin("chat") },
          { id: "board", label: t("board", lang), run: () => setActiveWin("board") },
          { id: "market", label: t("marketplace", lang), run: () => openMarket() },
          { id: "term", label: "Terminal", run: () => void spawnTerm() },
          { id: "settings", label: t("settings", lang), run: () => { setSettingsSection("appearance"); setSettingsOpen(true); } },
          { id: "memory", label: t("memory", lang), run: () => { setSettingsSection("memory"); setSettingsOpen(true); } },
          { id: "new", label: t("slashNew", lang), run: () => void openNewChat() },
        ] as PaletteAction[]}
      />

      <Toasts />

      {updateAvail && (
        <div className="update-banner">
          <span>🎉 Güncelleme mevcut: <strong>{updateAvail}</strong></span>
          <a
            href={`https://github.com/Kayra-ML/Rove_cli/releases/tag/${updateAvail}`}
            target="_blank" rel="noreferrer"
            className="update-link"
          >
            İndir
          </a>
          <button className="update-dismiss" onClick={() => setUpdateAvail(null)} title="Kapat">×</button>
        </div>
      )}

      <div className="status">
        <span style={{ color: rpcOk ? "var(--ok)" : "var(--bad)" }}>● {rpcOk ? t("connected", lang) : "offline"}</span>
        {workspace && (
          <span style={{ marginLeft: 4 }}>
            {workspace.name} / {workspace.defaultBranch}
          </span>
        )}
        <span style={{ marginLeft: "auto", color: "var(--faint)" }}>Rove Code</span>
      </div>
    </div>
  );
}
