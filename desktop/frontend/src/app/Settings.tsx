import { useCallback, useEffect, useState } from "react";
import { discoverSSH, pickFolder, rpc, type DiscoveredSSH } from "~/lib/rpc";
import { canConnectRemote, connectRemote, onConnectProgress } from "~/lib/connection";
import { toast } from "~/lib/toast";
import { notify, notifyEnabled, setNotifyEnabled } from "~/lib/notify";
import { useSkills, useProviders, useGoals, useWorkspaces } from "~/hooks/useApi";
import { ConnectPicker } from "./ConnectPicker";
import type { Goal, InstalledSkill, Provider, SSHTarget, TerminalSession, Workspace } from "~/lib/types";
import { AgentRoster, loadSSHHosts, removeSSHHost, saveSSHHost } from "./Popovers";
import { Permissions } from "./Permissions";
import { Automation } from "./Automation";
import { Memory } from "./Memory";
import { Icon } from "./Icons";
import { UsagePanel } from "./UsagePanel";
import { PALETTES, THEMES, applyTheme, applyLang, type Palette, type ThemeId } from "~/lib/themes";
import { LANGS, t, getLang, type Lang } from "~/lib/i18n";

import { OfficePanel, type OfficeIntent } from "./OfficePanel";
import { useDialog } from "~/hooks/useDialog";

export type Section = "appearance" | "workspace" | "providers" | "goals" | "agents" | "roster" | "permissions" | "automation" | "memory" | "mcp" | "webhook" | "profiles" | "servers" | "usage";

interface Props {
  onClose: () => void;
  onSelectWorkspace?: (ws: Workspace) => void;
  // Handing over a freshly SSH-opened shell — Settings only manages the
  // saved host list and starts the connection; Terminal mode owns the pane.
  onOpenShell?: (sess: TerminalSession) => void;
  initialSection?: Section;
  // Office: open the list, a new agent, or one agent to edit
  officeIntent?: OfficeIntent;
  workspace?: Workspace | null;
  sessionId?: string;
}

function hostLabel(h: SSHTarget): string {
  return `${h.user ? `${h.user}@` : ""}${h.host}:${h.port ?? 22}`;
}

export function Settings({ onClose, onSelectWorkspace, onOpenShell, initialSection = "appearance", officeIntent, workspace, sessionId }: Props) {
  const sheet = useDialog<HTMLDivElement>();
  const { skills, reload: reloadSkills } = useSkills();
  const { providers, reload: reloadProviders } = useProviders();
  // "add a provider" shows the systems a user can connect their account to
  const [adding, setAdding] = useState(false);
  const { goals, reload: reloadGoals } = useGoals();
  const { workspaces, reload: reloadWs } = useWorkspaces();
  const [section, setSection] = useState<Section>(initialSection);
  useEffect(() => { setSection(initialSection); }, [initialSection]);
  const [theme, setTheme] = useState<ThemeId>((localStorage.getItem("aether.theme") as ThemeId) || "graphite");
  const [lang, setLang] = useState<Lang>(getLang());
  const [newProvider, setNewProvider] = useState({ name: "", baseUrl: "", secretId: "", secret: "", models: "" });
  const [provBusy, setProvBusy] = useState<string | null>(null); // "new" or a provider id
  const [provErr, setProvErr] = useState("");
  const [notifyOn, setNotifyOn] = useState(notifyEnabled);
  const [goalTitle, setGoalTitle] = useState("");
  const [goalCriteria, setGoalCriteria] = useState("");

  // SSH: Settings owns the saved host list; connecting starts the shell and
  // hands it to Terminal mode, then closes here.
  const [sshHosts, setSshHosts] = useState<SSHTarget[]>([]);
  const [sshForm, setSshForm] = useState({ host: "", port: "22", user: "", authMethod: "agent", keyPath: "" });
  // SSH servers found on this computer, offered to add in one go
  const [found, setFound] = useState<DiscoveredSSH[] | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [finding, setFinding] = useState(false);
  // running Rove on a server: which host, and what it is doing
  const [runOn, setRunOn] = useState<string | null>(null);
  const [runStep, setRunStep] = useState("");
  useEffect(() => onConnectProgress(setRunStep), []);
  const runHere = useCallback(async (h: SSHTarget) => {
    setRunOn(h.host);
    setRunStep("");
    setSshErr("");
    try {
      await connectRemote(h); // reloads into the server
    } catch (e) {
      setSshErr(e instanceof Error ? e.message : String(e));
      setRunOn(null);
    }
  }, []);
  const [sshBusy, setSshBusy] = useState(false);
  const [sshErr, setSshErr] = useState("");
  useEffect(() => { if (section === "servers") setSshHosts(loadSSHHosts()); }, [section]);

  // a found server as a saved one: a config alias is kept as the alias, so
  // ssh uses the whole config entry; a known_hosts entry keeps its port
  const targetOf = (d: DiscoveredSSH): SSHTarget => d.alias
    ? { host: d.alias, hostName: d.hostName, authMethod: "agent" }
    : { host: d.hostName, port: d.port, authMethod: "agent" };
  const foundKey = (d: DiscoveredSSH) => d.alias ?? `${d.hostName}:${d.port ?? ""}`;
  const isSaved = (d: DiscoveredSSH) => sshHosts.some((h) => h.host === (d.alias ?? d.hostName));
  const findServers = useCallback(async () => {
    setFinding(true);
    setSshErr("");
    try {
      const list = await discoverSSH();
      setFound(list);
      // new config entries are ticked; bare known_hosts are left to choose
      setPicked(new Set(list.filter((d) => d.source === "config" && !sshHosts.some((h) => h.host === (d.alias ?? d.hostName))).map(foundKey)));
    } catch (e) {
      setSshErr(e instanceof Error ? e.message : String(e));
    } finally {
      setFinding(false);
    }
  }, [sshHosts]);
  const addFound = () => {
    let list = sshHosts;
    for (const d of found ?? []) {
      if (picked.has(foundKey(d)) && !isSaved(d)) list = saveSSHHost(targetOf(d));
    }
    setSshHosts(list);
    toast(`${picked.size} ${t("sshAdded", lang)}`, "ok");
    setFound(null);
    setPicked(new Set());
  };
  const connectHost = useCallback(async (target: SSHTarget, resetForm = false) => {
    setSshBusy(true);
    setSshErr("");
    try {
      const sess = await rpc<TerminalSession>("ssh.open", target);
      setSshHosts(saveSSHHost(target));
      if (resetForm) setSshForm({ host: "", port: "22", user: "", authMethod: "agent", keyPath: "" });
      onOpenShell?.(sess);
      onClose();
    } catch (e) {
      setSshErr(e instanceof Error ? e.message : "ssh failed");
    } finally {
      setSshBusy(false);
    }
  }, [onOpenShell, onClose]);

  // MCP state
  type MCPServer = { id: string; name: string; command: string; args: string[]; env: Record<string, string> };
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([]);
  const [mcpForm, setMcpForm] = useState({ name: "", command: "", args: "", env: "" });
  const reloadMCP = useCallback(async () => {
    const list = await rpc<MCPServer[]>("mcp.list", {});
    setMcpServers(list ?? []);
  }, []);
  useEffect(() => { if (section === "mcp") void reloadMCP(); }, [section, reloadMCP]);

  const pickTheme = (id: ThemeId) => {
    setTheme(id);
    applyTheme(id);
  };
  const pickLang = (id: Lang) => {
    setLang(id);
    applyLang(id);
  };

  // Saving a provider asks it for its models ({base}/models) unless they
  // are typed in; the key is stored first so that request can use it.
  type Saved = Provider & { modelsFetched?: number; modelsError?: string };
  const saveProvider = useCallback(async () => {
    const name = newProvider.name.trim();
    if (!name || provBusy) return;
    setProvBusy("new");
    setProvErr("");
    try {
      const secretId = newProvider.secretId.trim() || `${name}-key`;
      if (newProvider.secret) await rpc("secret.put", { id: secretId, value: newProvider.secret });
      const typed = newProvider.models.split(/[,\n]/).map((m) => m.trim()).filter(Boolean);
      const saved = await rpc<Saved>("provider.upsert", {
        name,
        kind: "openai-compat",
        baseUrl: newProvider.baseUrl.trim(),
        secretId,
        models: typed,
      });
      if (saved.modelsError) {
        setProvErr(`${t("provModelsFailed", lang)}: ${saved.modelsError}`);
      } else {
        toast(`${name}: ${saved.models?.length ?? 0} ${t("provModelsAdded", lang)}`, "ok");
        setNewProvider({ name: "", baseUrl: "", secretId: "", secret: "", models: "" });
      }
      await reloadProviders();
    } catch (e) {
      setProvErr(e instanceof Error ? e.message : String(e));
    } finally {
      setProvBusy(null);
    }
  }, [newProvider, provBusy, reloadProviders, lang]);

  const refreshModels = useCallback(async (p: Provider) => {
    setProvBusy(p.id);
    setProvErr("");
    try {
      const r = await rpc<Saved>("provider.refreshModels", { id: p.id });
      toast(`${p.name}: ${r.models?.length ?? 0} ${t("provModelsAdded", lang)}`, "ok");
      await reloadProviders();
    } catch (e) {
      setProvErr(`${p.name} — ${t("provModelsFailed", lang)}: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setProvBusy(null);
    }
  }, [reloadProviders, lang]);

  const openWorkspace = useCallback(async () => {
    const path = (await pickFolder()).trim();
    if (!path) return;
    const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
    const ws = await rpc<Workspace>("workspace.open", { path, name });
    await reloadWs();
    onSelectWorkspace?.(ws);
  }, [reloadWs, onSelectWorkspace]);

  const uninstallSkill = useCallback(
    async (name: string) => {
      await rpc("skill.uninstall", { name });
      await reloadSkills();
    },
    [reloadSkills],
  );

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-sheet" role="dialog" aria-modal="true" aria-label={t("settings", lang)} ref={sheet} onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h1>{t("settings", lang)}</h1>
          <button className="icon-btn" onClick={onClose} title={t("close", lang)}>✕</button>
        </header>
        <div className="split" style={{ height: "calc(100% - 52px)" }}>
          <aside className="split-rail">
            <button className={`rail-item${section === "appearance" ? " active" : ""}`} onClick={() => setSection("appearance")}>
              <Icon name="palette" size={15} /> {t("appearance", lang)}
            </button>
            <button className={`rail-item${section === "profiles" ? " active" : ""}`} onClick={() => setSection("profiles")}>
              <Icon name="agents" size={15} /> {t("profiles", lang)}
            </button>
            <button className={`rail-item${section === "usage" ? " active" : ""}`} onClick={() => setSection("usage")}>
              <Icon name="pulse" size={15} /> {t("usageTitle", lang)}
            </button>
            <button className={`rail-item${section === "providers" ? " active" : ""}`} onClick={() => setSection("providers")}>
              <Icon name="key" size={15} /> {t("providers", lang)}
              {providers.length > 0 && <span className="count">{providers.length}</span>}
            </button>
            <button className={`rail-item${section === "workspace" ? " active" : ""}`} onClick={() => setSection("workspace")}>
              <Icon name="workspace" size={15} /> {t("workspaces", lang)}
              {workspaces.length > 0 && <span className="count">{workspaces.length}</span>}
            </button>
            <button className={`rail-item${section === "goals" ? " active" : ""}`} onClick={() => setSection("goals")}>
              <Icon name="flag" size={15} /> {t("goals", lang)}
              {goals.length > 0 && <span className="count">{goals.length}</span>}
            </button>
            <button className={`rail-item${section === "roster" ? " active" : ""}`} onClick={() => setSection("roster")}>
              <Icon name="agents" size={15} /> {t("roster", lang)}
            </button>
            <button className={`rail-item${section === "agents" ? " active" : ""}`} onClick={() => setSection("agents")}>
              <Icon name="box" size={15} /> {t("skills", lang)}
              {skills.length > 0 && <span className="count">{skills.length}</span>}
            </button>
            <button className={`rail-item${section === "permissions" ? " active" : ""}`} onClick={() => setSection("permissions")}>
              <Icon name="key" size={15} /> {t("permissions", lang)}
            </button>
            <button className={`rail-item${section === "automation" ? " active" : ""}`} onClick={() => setSection("automation")}>
              <Icon name="pulse" size={15} /> {t("automation", lang)}
            </button>
            <button className={`rail-item${section === "servers" ? " active" : ""}`} onClick={() => setSection("servers")}>
              <Icon name="terminal" size={15} /> {t("servers", lang)}
              {sshHosts.length > 0 && <span className="count">{sshHosts.length}</span>}
            </button>
            <button className={`rail-item${section === "memory" ? " active" : ""}`} onClick={() => setSection("memory")}>
              <Icon name="box" size={15} /> {t("memory", lang)}
            </button>
            <button className={`rail-item${section === "mcp" ? " active" : ""}`} onClick={() => setSection("mcp")}>
              <Icon name="pulse" size={15} /> MCP
              {mcpServers.length > 0 && <span className="count">{mcpServers.length}</span>}
            </button>
          </aside>

          <div className="split-body">
            {section === "appearance" && (
              <>
                <h2>{t("appearance", lang)}</h2>
                <p className="split-lead">{t("appearanceLead", lang)}</p>
                <div className="set-card">
                  <div className="set-row set-row-stack">
                    <div className="set-text">
                      <strong>{t("theme", lang)}</strong>
                      <span>{THEMES.find((th) => th.id === theme)?.label} · {t("themeHint", lang)}</span>
                    </div>
                    <div className="theme-mini" role="radiogroup" aria-label={t("theme", lang)}>
                      {THEMES.map((th) => (
                        <button
                          key={th.id}
                          type="button"
                          role="radio"
                          aria-checked={theme === th.id}
                          title={th.label}
                          className={`theme-tile${theme === th.id ? " active" : ""}`}
                          onClick={() => pickTheme(th.id)}
                        >
                          <ThemePreview p={PALETTES[th.id]} />
                          <span className="theme-name">{th.label}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                  <div className="set-row">
                    <div className="set-text">
                      <strong>{t("language", lang)}</strong>
                      <span>{t("languageHint", lang)}</span>
                    </div>
                    <select className="set-select" aria-label={t("language", lang)} value={lang} onChange={(e) => pickLang(e.target.value as Lang)}>
                      {LANGS.map((l) => <option key={l.id} value={l.id}>{l.label}</option>)}
                    </select>
                  </div>
                  <div className="set-row">
                    <div className="set-text">
                      <strong>{t("notifications", lang)}</strong>
                      <span>{t("notificationsHint", lang)}</span>
                    </div>
                    <div className="set-controls">
                      <button type="button" className="ghost set-small" disabled={!notifyOn} onClick={() => void notify("Rove", t("notifyRunDone", lang), true)}>{t("notifyTest", lang)}</button>
                      <label className="switch" title={t("notifications", lang)}>
                        <input type="checkbox" aria-label={t("notifications", lang)} checked={notifyOn} onChange={(e) => { setNotifyEnabled(e.target.checked); setNotifyOn(e.target.checked); }} />
                        <span className="switch-track"><span className="switch-knob" /></span>
                      </label>
                    </div>
                  </div>
                </div>
              </>
            )}

            {section === "providers" && (
              <>
                <div className="con-title-row">
                  <h2>{t("providers", lang)}</h2>
                  {!adding && <button type="button" className="primary" onClick={() => setAdding(true)}>+ {t("conAdd", lang)}</button>}
                </div>
                <p className="split-lead">{t("providersLead", lang)}</p>
                {adding && (
                  <ConnectPicker lang={lang} providers={providers} onClose={() => setAdding(false)}
                    onDone={async () => { await reloadProviders(); setAdding(false); }} />
                )}
                {adding ? null : providers.length === 0 ? (
                  <div className="empty">
                    <strong>{t("providersEmpty", lang)}</strong>
                    <p>{t("providersEmptyHint", lang)}</p>
                  </div>
                ) : (
                  <div className="row-list" style={{ marginBottom: 22 }}>
                    {providers.map((p: Provider) => {
                      const models = p.models ?? [];
                      return (
                        <div key={p.id} className="row prov-row" data-provider={p.name}>
                          <div className="meta">
                            <strong>{p.name}{p.default && <span className="prov-default"> · {t("slashModelDefaultProvider", lang)}</span>}</strong>
                            <span>
                              {p.kind === "agent-cli"
                                ? `${t("conViaAccount", lang)} · ${t(p.baseUrl.includes("access=full") ? "conAccessFull" : "conAccessEdits", lang)}`
                                : p.baseUrl || p.kind}
                              {" · "}{models.length ? `${models.length} ${t("provModelCount", lang)}` : t("provNoModels", lang)}
                            </span>
                            {models.length > 0 && (
                              <span className="prov-models" title={models.join("\n")}>
                                {models.slice(0, 8).map((m) => <code key={m}>{m}</code>)}
                                {models.length > 8 && <code className="more">+{models.length - 8}</code>}
                              </span>
                            )}
                          </div>
                          {p.kind !== "fake" && (
                            <button className="ghost prov-refresh" disabled={provBusy !== null} title={t("provRefreshHint", lang)} onClick={() => void refreshModels(p)}>
                              {provBusy === p.id ? t("provFetching", lang) : `↻ ${t("provRefresh", lang)}`}
                            </button>
                          )}
                          <button className="danger-btn" style={{ fontSize: 11 }} onClick={() => void rpc("provider.delete", { id: p.id }).then(() => reloadProviders())}>{t("delete", lang)}</button>
                        </div>
                      );
                    })}
                  </div>
                )}
                {!adding && (
                <details className="con-custom">
                <summary>{t("conCustom", lang)}</summary>
                <div className="form-stack">
                  <label>{t("fieldName", lang)}</label>
                  <input placeholder="my-provider" value={newProvider.name} onChange={(e) => setNewProvider((p) => ({ ...p, name: e.target.value }))} />
                  <label>{t("providerBaseUrl", lang)}</label>
                  <input placeholder="https://api.example.com/v1" value={newProvider.baseUrl} onChange={(e) => setNewProvider((p) => ({ ...p, baseUrl: e.target.value }))} />
                  <label>{t("providerSecretId", lang)}</label>
                  <input placeholder="provider-key" value={newProvider.secretId} onChange={(e) => setNewProvider((p) => ({ ...p, secretId: e.target.value }))} />
                  <label>{t("providerApiKey", lang)}</label>
                  <input type="password" placeholder="sk-…" value={newProvider.secret} onChange={(e) => setNewProvider((p) => ({ ...p, secret: e.target.value }))} />
                  <label>{t("provModelsLabel", lang)}</label>
                  <input placeholder={t("provModelsPlaceholder", lang)} value={newProvider.models} onChange={(e) => setNewProvider((p) => ({ ...p, models: e.target.value }))} />
                  <span className="map-muted" style={{ fontSize: 11.5 }}>{t("provModelsHint", lang)}</span>
                  {provErr && <div className="prov-err">{provErr}</div>}
                  <button className="primary" disabled={!newProvider.name.trim() || provBusy !== null} onClick={() => void saveProvider()} style={{ alignSelf: "flex-start", marginTop: 6 }}>
                    {provBusy === "new" ? t("provFetching", lang) : t("save", lang)}
                  </button>
                </div>
                </details>
                )}
              </>
            )}

            {section === "workspace" && (
              <>
                <h2>{t("workspaces", lang)}</h2>
                <p className="split-lead">{t("workspacesLead", lang)}</p>
                {workspaces.length === 0 ? (
                  <div className="empty">
                    <strong>{t("workspacesEmpty", lang)}</strong>
                    <p>{t("workspacesEmptyHint", lang)}</p>
                  </div>
                ) : (
                  <div className="row-list" style={{ marginBottom: 18 }}>
                    {workspaces.map((ws: Workspace) => (
                      <div
                        key={ws.id}
                        className="row"
                        style={{ cursor: "pointer" }}
                        onClick={() => { onSelectWorkspace?.(ws); onClose(); }}
                      >
                        <div className="avatar"><Icon name="workspace" size={14} /></div>
                        <div className="meta">
                          <strong>{ws.name}</strong>
                          <span>{ws.path} · {ws.defaultBranch}</span>
                        </div>
                        <button className="danger-btn" style={{ fontSize: 11 }} onClick={(e) => { e.stopPropagation(); void rpc("workspace.delete", { id: ws.id }).then(() => reloadWs()); }}>{t("delete", lang)}</button>
                      </div>
                    ))}
                  </div>
                )}
                <button className="primary" onClick={openWorkspace}>{t("openFolder", lang)}</button>
              </>
            )}

            {section === "goals" && (
              <>
                <h2>{t("goals", lang)}</h2>
                <p className="split-lead">{t("goalsLead", lang)}</p>
                {goals.length === 0 ? (
                  <div className="empty">
                    <strong>{t("goalsEmpty", lang)}</strong>
                    <p>{t("goalsEmptyHint", lang)}</p>
                  </div>
                ) : (
                  <div className="row-list" style={{ marginBottom: 18 }}>
                    {goals.map((g: Goal) => (
                      <div key={g.id} className="row">
                        <span className={`pip${g.status === "running" ? " run" : g.status === "done" ? " on" : ""}`} />
                        <div className="meta">
                          <strong>{g.title}</strong>
                          <span>{g.status} · iter {g.iteration}</span>
                        </div>
                        <button
                          className="primary"
                          style={{ fontSize: 11 }}
                          disabled={g.status === "running"}
                          onClick={() => void rpc("goal.drive", {
                            id: g.id,
                            sessionId: sessionId ?? "",
                            workspace: workspace?.path ?? "",
                          }).then(() => reloadGoals())}
                        >
                          ▶
                        </button>
                        <button className="danger-btn" style={{ fontSize: 11 }} onClick={() => void rpc("goal.delete", { id: g.id }).then(() => reloadGoals())}>{t("delete", lang)}</button>
                      </div>
                    ))}
                  </div>
                )}
                <div className="form-stack">
                  <label>{t("goalTitle", lang)}</label>
                  <input value={goalTitle} onChange={(e) => setGoalTitle(e.target.value)} placeholder={t("goalTitleHint", lang)} />
                  <label>{t("goalCriteria", lang)}</label>
                  <input value={goalCriteria} onChange={(e) => setGoalCriteria(e.target.value)} placeholder={t("goalCriteriaHint", lang)} />
                <button
                  className="primary"
                  onClick={async () => {
                    if (!goalTitle.trim()) return;
                    await rpc("goal.create", {
                      title: goalTitle.trim(),
                      workspaceId: workspace?.id ?? "",
                      completionContract: {
                        criteria: goalCriteria.split(",").map((s) => s.trim()).filter(Boolean),
                        maxIterations: 8,
                      },
                    });
                    setGoalTitle("");
                    setGoalCriteria("");
                    await reloadGoals();
                  }}
                >
                  {t("create", lang)}
                </button>
                </div>
              </>
            )}

            {section === "permissions" && <Permissions />}

            {section === "usage" && <UsagePanel />}

            {section === "automation" && <Automation />}

            {section === "profiles" && <OfficePanel intent={officeIntent} />}

            {section === "servers" && (
              <>
                <h2>{t("servers", lang)}</h2>
                <p className="split-lead">{t("sshLead", lang)}</p>
                {sshHosts.length === 0 ? (
                  <div className="empty">
                    <strong>{t("sshEmpty", lang)}</strong>
                    <p>{t("sshEmptyHint", lang)}</p>
                  </div>
                ) : (
                  <div className="row-list" style={{ marginBottom: 22 }}>
                    {sshHosts.map((h) => (
                      <div key={hostLabel(h)} className="row">
                        <div className="avatar"><Icon name="terminal" size={14} /></div>
                        <div className="meta">
                          <strong>{h.user ? `${h.user}@${h.host}` : h.host}</strong>
                          <span>{h.hostName ? `${h.hostName} · ~/.ssh/config` : `:${h.port ?? 22} · ${h.authMethod || "agent"}`}</span>
                        </div>
                        {canConnectRemote() && (
                          <button className="ghost" style={{ fontSize: 11 }} disabled={runOn !== null} title={t("connRunHereHint", lang)} onClick={() => void runHere(h)}>
                            {runOn === h.host ? (runStep || t("connConnecting", lang)) : t("connRunHere", lang)}
                          </button>
                        )}
                        <button className="primary" style={{ fontSize: 11 }} disabled={sshBusy} onClick={() => void connectHost(h)}>
                          {sshBusy ? "…" : t("sshShell", lang)}
                        </button>
                        <button className="danger-btn" style={{ fontSize: 11 }} onClick={() => setSshHosts(removeSSHHost(h))}>{t("delete", lang)}</button>
                      </div>
                    ))}
                  </div>
                )}
                <div className="ssh-find">
                  <button type="button" className="ghost" disabled={finding} onClick={() => void findServers()}>
                    {finding ? "…" : `⌕ ${t("sshFind", lang)}`}
                  </button>
                  <span className="map-muted">{t("sshFindHint", lang)}</span>
                </div>
                {found && (
                  <div className="ssh-found" role="group" aria-label={t("sshFound", lang)}>
                    {found.length === 0 ? (
                      <p className="map-muted">{t("sshNoneFound", lang)}</p>
                    ) : (
                      <>
                        {found.map((d) => {
                          const saved = isSaved(d);
                          const k = foundKey(d);
                          return (
                            <label key={k} className={`ssh-found-row${saved ? " saved" : ""}`}>
                              <input
                                type="checkbox"
                                disabled={saved}
                                checked={saved || picked.has(k)}
                                onChange={(e) => setPicked((cur) => { const n = new Set(cur); if (e.target.checked) n.add(k); else n.delete(k); return n; })}
                              />
                              <span className="ssh-found-name">
                                <strong>{d.alias ?? d.hostName}</strong>
                                <span>
                                  {d.alias && d.hostName !== d.alias ? `${d.user ? `${d.user}@` : ""}${d.hostName}` : d.user ? `${d.user}@${d.hostName}` : ""}
                                  {d.port ? `:${d.port}` : ""}
                                </span>
                              </span>
                              <span className="ssh-found-src">{saved ? t("sshSaved", lang) : d.source === "config" ? "~/.ssh/config" : "known_hosts"}</span>
                            </label>
                          );
                        })}
                        <div className="ssh-found-actions">
                          <button type="button" className="ghost" onClick={() => setFound(null)}>{t("cancel", lang)}</button>
                          <button type="button" className="primary" disabled={picked.size === 0} onClick={addFound}>
                            {t("sshAddPicked", lang)} ({picked.size})
                          </button>
                        </div>
                      </>
                    )}
                  </div>
                )}
                <div className="form-stack">
                  <label>{t("sshHost", lang)}</label>
                  <input value={sshForm.host} placeholder="example.com" onChange={(e) => setSshForm((f) => ({ ...f, host: e.target.value }))} />
                  <label>{t("sshPort", lang)}</label>
                  <input value={sshForm.port} placeholder="22" onChange={(e) => setSshForm((f) => ({ ...f, port: e.target.value }))} />
                  <label>{t("sshUser", lang)}</label>
                  <input value={sshForm.user} placeholder="root" onChange={(e) => setSshForm((f) => ({ ...f, user: e.target.value }))} />
                  <label>{t("sshAuth", lang)}</label>
                  <select value={sshForm.authMethod} onChange={(e) => setSshForm((f) => ({ ...f, authMethod: e.target.value }))}>
                    <option value="agent">ssh-agent</option>
                    <option value="key">private key</option>
                    <option value="password">password (prompt in pty)</option>
                  </select>
                  {sshForm.authMethod === "key" && (
                    <>
                      <label>{t("sshKeyPath", lang)}</label>
                      <input value={sshForm.keyPath} placeholder="~/.ssh/id_ed25519" onChange={(e) => setSshForm((f) => ({ ...f, keyPath: e.target.value }))} />
                    </>
                  )}
                  {sshErr && <div className="danger">{sshErr}</div>}
                  <button
                    className="primary"
                    style={{ alignSelf: "flex-start", marginTop: 6 }}
                    disabled={sshBusy || !sshForm.host.trim()}
                    onClick={() => void connectHost({
                      host: sshForm.host.trim(),
                      port: Number(sshForm.port) || 22,
                      user: sshForm.user.trim() || undefined,
                      authMethod: sshForm.authMethod,
                      keyPath: sshForm.keyPath.trim() || undefined,
                    }, true)}
                  >
                    {sshBusy ? "…" : "Kaydet ve bağlan"}
                  </button>
                </div>
              </>
            )}

            {section === "memory" && <Memory workspaceId={workspace?.id} sessionId={sessionId} />}

            {section === "roster" && <AgentRoster />}

            {section === "agents" && (
              <>
                <h2>{t("skills", lang)}</h2>
                <p className="split-lead">{t("skillsLead", lang)}</p>
                {skills.length === 0 ? (
                  <div className="empty">
                    <strong>{t("skillsEmpty", lang)}</strong>
                    <p>{t("skillsEmptyHint", lang)}</p>
                  </div>
                ) : (
                  <div className="row-list">
                    {skills.map((s: InstalledSkill) => (
                      <div key={s.manifest.name} className="row">
                        <div className="avatar"><Icon name="box" size={14} /></div>
                        <div className="meta">
                          <strong>{s.manifest.name} <span style={{ color: "var(--faint)", fontWeight: 400 }}>v{s.manifest.version}</span></strong>
                          <span>{s.manifest.description}</span>
                        </div>
                        <button className="ghost" style={{ fontSize: 11 }} onClick={() => void rpc("skill.setEnabled", { name: s.manifest.name, enabled: !s.enabled }).then(() => reloadSkills())}>
                          {t(s.enabled ? "enabled" : "disabled", lang)}
                        </button>
                        <button className="danger-btn" style={{ fontSize: 11 }} onClick={() => uninstallSkill(s.manifest.name)}>{t("delete", lang)}</button>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}

            {section === "mcp" && (
              <>
                <h2>{t("mcpServers", lang)}</h2>
                <p className="split-lead">{t("mcpLead", lang)}</p>
                {mcpServers.length === 0 ? (
                  <div className="empty">
                    <strong>{t("mcpEmpty", lang)}</strong>
                    <p>{t("mcpEmptyHint", lang)}</p>
                  </div>
                ) : (
                  <div className="row-list" style={{ marginBottom: 22 }}>
                    {mcpServers.map((srv) => (
                      <div key={srv.id} className="row">
                        <div className="avatar"><Icon name="pulse" size={14} /></div>
                        <div className="meta">
                          <strong>{srv.name}</strong>
                          <span>{srv.command} {(srv.args ?? []).join(" ")}</span>
                        </div>
                        <button
                          className="danger-btn"
                          style={{ fontSize: 11 }}
                          onClick={() => void rpc("mcp.remove", { id: srv.id }).then(() => reloadMCP())}
                        >
                          {t("delete", lang)}
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                <div className="form-stack">
                  <label>{t("fieldName", lang)}</label>
                  <input placeholder="my-mcp-server" value={mcpForm.name} onChange={(e) => setMcpForm((f) => ({ ...f, name: e.target.value }))} />
                  <label>{t("mcpCommand", lang)}</label>
                  <input placeholder="npx @modelcontextprotocol/server-filesystem" value={mcpForm.command} onChange={(e) => setMcpForm((f) => ({ ...f, command: e.target.value }))} />
                  <label>{t("mcpArgs", lang)}</label>
                  <input placeholder="/home/user/workspace" value={mcpForm.args} onChange={(e) => setMcpForm((f) => ({ ...f, args: e.target.value }))} />
                  <label>{t("mcpEnv", lang)}</label>
                  <input placeholder="TOKEN=abc,DEBUG=1" value={mcpForm.env} onChange={(e) => setMcpForm((f) => ({ ...f, env: e.target.value }))} />
                  <button
                    className="primary"
                    style={{ alignSelf: "flex-start", marginTop: 6 }}
                    onClick={async () => {
                      if (!mcpForm.name || !mcpForm.command) return;
                      const args = mcpForm.args.trim() ? mcpForm.args.trim().split(/\s+/) : [];
                      const env: Record<string, string> = {};
                      for (const pair of mcpForm.env.split(",")) {
                        const [k, ...v] = pair.trim().split("=");
                        if (k) env[k] = v.join("=");
                      }
                      await rpc("mcp.add", { name: mcpForm.name, command: mcpForm.command, args, env });
                      setMcpForm({ name: "", command: "", args: "", env: "" });
                      await reloadMCP();
                    }}
                  >
                    {t("mcpAdd", lang)}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
// A theme tile's picture: a tiny window with its side panel, its work area,
// a line of text in each and an accent button — the colors as they meet.
function ThemePreview({ p }: { p: Palette }) {
  return (
    <span className="theme-prev" style={{ background: p.bg, borderColor: p.border }} aria-hidden>
      <span className="theme-prev-side" style={{ background: p.side.bg }}>
        <i style={{ background: p.side.accent }} />
        <i style={{ background: p.side.muted }} />
        <i style={{ background: p.side.faint }} />
      </span>
      <span className="theme-prev-main" style={{ background: p.raised }}>
        <i style={{ background: p.text }} />
        <i style={{ background: p.muted }} />
        <b style={{ background: p.accent }} />
      </span>
    </span>
  );
}
