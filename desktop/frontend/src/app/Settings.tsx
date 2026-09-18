import { useCallback, useEffect, useState } from "react";
import { pickFolder, rpc } from "~/lib/rpc";
import { useSkills, useProviders, useGoals, useWorkspaces } from "~/hooks/useApi";
import type { Agent, Goal, InstalledSkill, Provider, WebhookRule, Workspace } from "~/lib/types";
import { AgentRoster } from "./Popovers";
import { Permissions } from "./Permissions";
import { Automation } from "./Automation";
import { Memory } from "./Memory";
import { Icon } from "./Icons";
import { THEMES, applyTheme, applyLang, type ThemeId } from "~/lib/themes";
import { LANGS, t, getLang, type Lang } from "~/lib/i18n";

import { ProfilePanel } from "./ProfilePanel";

type Section = "appearance" | "workspace" | "providers" | "goals" | "agents" | "roster" | "permissions" | "automation" | "memory" | "mcp" | "webhook" | "profiles";

interface Props {
  onClose: () => void;
  onSelectWorkspace?: (ws: Workspace) => void;
  initialSection?: Section;
  workspace?: Workspace | null;
  sessionId?: string;
}

export function Settings({ onClose, onSelectWorkspace, initialSection = "appearance", workspace, sessionId }: Props) {
  const { skills, reload: reloadSkills } = useSkills();
  const { providers, reload: reloadProviders } = useProviders();
  const { goals, reload: reloadGoals } = useGoals();
  const { workspaces, reload: reloadWs } = useWorkspaces();
  const [section, setSection] = useState<Section>(initialSection);
  useEffect(() => { setSection(initialSection); }, [initialSection]);
  const [theme, setTheme] = useState<ThemeId>((localStorage.getItem("aether.theme") as ThemeId) || "aether");
  const [lang, setLang] = useState<Lang>(getLang());
  const [newProvider, setNewProvider] = useState({ name: "", baseUrl: "", secretId: "", secret: "" });
  const [goalTitle, setGoalTitle] = useState("");
  const [goalCriteria, setGoalCriteria] = useState("");

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

  const saveProvider = useCallback(async () => {
    if (!newProvider.name) return;
    const secretId = newProvider.secretId || `${newProvider.name}-key`;
    await rpc("provider.upsert", {
      name: newProvider.name,
      kind: "openai-compat",
      baseUrl: newProvider.baseUrl,
      secretId,
      models: [],
    });
    if (newProvider.secret) await rpc("secret.put", { id: secretId, value: newProvider.secret });
    setNewProvider({ name: "", baseUrl: "", secretId: "", secret: "" });
    await reloadProviders();
  }, [newProvider, reloadProviders]);

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
      <div className="modal-sheet" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h1>{t("settings", lang)}</h1>
          <button className="icon-btn" onClick={onClose} title={t("close", lang)}>✕</button>
        </header>
        <div className="split" style={{ height: "calc(100% - 52px)" }}>
          <aside className="split-rail">
            <button className={`rail-item${section === "appearance" ? " active" : ""}`} onClick={() => setSection("appearance")}>
              <Icon name="palette" size={15} /> {t("appearance", lang)}
            </button>
            <button className={`rail-item${section === "providers" ? " active" : ""}`} onClick={() => setSection("providers")}>
              <Icon name="key" size={15} /> {t("providers", lang)}
              <span className="count">{providers.length}</span>
            </button>
            <button className={`rail-item${section === "workspace" ? " active" : ""}`} onClick={() => setSection("workspace")}>
              <Icon name="workspace" size={15} /> {t("workspaces", lang)}
              <span className="count">{workspaces.length}</span>
            </button>
            <button className={`rail-item${section === "goals" ? " active" : ""}`} onClick={() => setSection("goals")}>
              <Icon name="flag" size={15} /> {t("goals", lang)}
              <span className="count">{goals.length}</span>
            </button>
            <button className={`rail-item${section === "roster" ? " active" : ""}`} onClick={() => setSection("roster")}>
              <Icon name="agents" size={15} /> {t("roster", lang)}
            </button>
            <button className={`rail-item${section === "agents" ? " active" : ""}`} onClick={() => setSection("agents")}>
              <Icon name="box" size={15} /> {t("skills", lang)}
              <span className="count">{skills.length}</span>
            </button>
            <button className={`rail-item${section === "permissions" ? " active" : ""}`} onClick={() => setSection("permissions")}>
              <Icon name="key" size={15} /> {t("permissions", lang)}
            </button>
            <button className={`rail-item${section === "automation" ? " active" : ""}`} onClick={() => setSection("automation")}>
              <Icon name="pulse" size={15} /> {t("automation", lang)}
            </button>
            <button className={`rail-item${section === "profiles" ? " active" : ""}`} onClick={() => setSection("profiles")}>
              <Icon name="agents" size={15} /> Profiller
            </button>
            <button className={`rail-item${section === "memory" ? " active" : ""}`} onClick={() => setSection("memory")}>
              <Icon name="box" size={15} /> {t("memory", lang)}
            </button>
            <button className={`rail-item${section === "mcp" ? " active" : ""}`} onClick={() => setSection("mcp")}>
              <Icon name="pulse" size={15} /> MCP
              <span className="count">{mcpServers.length}</span>
            </button>
          </aside>

          <div className="split-body">
            {section === "appearance" && (
              <>
                <h2>{t("theme", lang)}</h2>
                <p className="split-lead">16 palet. Tıklayınca anında uygulanır, localStorage’da kalır.</p>
                <div className="theme-grid">
                  {THEMES.map((th) => (
                    <button
                      key={th.id}
                      className={`theme-card${theme === th.id ? " active" : ""}`}
                      onClick={() => pickTheme(th.id)}
                    >
                      <div className="theme-swatch">
                        {th.swatch.map((c) => (
                          <span key={c} style={{ background: c }} />
                        ))}
                      </div>
                      <strong>{th.label}</strong>
                    </button>
                  ))}
                </div>
                <h2>{t("language", lang)}</h2>
                <p className="split-lead">Arayüz dili. Arapça seçilince RTL açılır.</p>
                <div className="lang-grid">
                  {LANGS.map((l) => (
                    <button
                      key={l.id}
                      className={`lang-chip${lang === l.id ? " active" : ""}`}
                      onClick={() => pickLang(l.id)}
                    >
                      {l.label}
                    </button>
                  ))}
                </div>
              </>
            )}

            {section === "providers" && (
              <>
                <h2>{t("providers", lang)}</h2>
                <p className="split-lead">Bağlı model sunucuları. Key OS keychain’de durur.</p>
                {providers.length === 0 ? (
                  <div className="empty">
                    <strong>Henüz provider yok</strong>
                    <p>OpenAI-uyumlu bir endpoint ekle — Groq, OpenRouter, local llama, kendi gateway’in.</p>
                  </div>
                ) : (
                  <div className="row-list" style={{ marginBottom: 22 }}>
                    {providers.map((p: Provider) => (
                      <div key={p.id} className="row">
                        <div className="avatar">{p.name.slice(0, 1).toUpperCase()}</div>
                        <div className="meta">
                          <strong>{p.name}</strong>
                          <span>{p.kind} · {(p.models ?? []).join(", ") || "model listesi yok"}</span>
                        </div>
                        <span className="pip on" />
                        <button className="danger-btn" style={{ fontSize: 11 }} onClick={() => void rpc("provider.delete", { id: p.id }).then(() => reloadProviders())}>{t("delete", lang)}</button>
                      </div>
                    ))}
                  </div>
                )}
                <div className="form-stack">
                  <label>Name</label>
                  <input placeholder="grok" value={newProvider.name} onChange={(e) => setNewProvider((p) => ({ ...p, name: e.target.value }))} />
                  <label>Base URL</label>
                  <input placeholder="https://api.x.ai/v1" value={newProvider.baseUrl} onChange={(e) => setNewProvider((p) => ({ ...p, baseUrl: e.target.value }))} />
                  <label>Secret ID</label>
                  <input placeholder="xai-key" value={newProvider.secretId} onChange={(e) => setNewProvider((p) => ({ ...p, secretId: e.target.value }))} />
                  <label>API key</label>
                  <input type="password" placeholder="sk-…" value={newProvider.secret} onChange={(e) => setNewProvider((p) => ({ ...p, secret: e.target.value }))} />
                  <button className="primary" onClick={saveProvider} style={{ alignSelf: "flex-start", marginTop: 6 }}>{t("save", lang)}</button>
                </div>
              </>
            )}

            {section === "workspace" && (
              <>
                <h2>{t("workspaces", lang)}</h2>
                <p className="split-lead">Açık repo klasörleri. Yeni path buradan eklenir.</p>
                {workspaces.length === 0 ? (
                  <div className="empty">
                    <strong>Workspace yok</strong>
                    <p>Bir git klasörü aç — agent orada çalışır.</p>
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
                <p className="split-lead">Judge onaylayana kadar dönen uzun işler. Her birinin kendi harness profili var.</p>
                {goals.length === 0 ? (
                  <div className="empty">
                    <strong>Çalışan goal yok</strong>
                    <p>Completion contract ile bir goal yarat. Engine, judge done diyene kadar iterasyon atar.</p>
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
                <div className="form-stack" style={{ marginBottom: 12 }}>
                  <label>{t("goals", lang)}</label>
                  <input value={goalTitle} onChange={(e) => setGoalTitle(e.target.value)} placeholder="title" />
                  <input value={goalCriteria} onChange={(e) => setGoalCriteria(e.target.value)} placeholder="criteria, comma separated" />
                </div>
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
              </>
            )}

            {section === "permissions" && <Permissions />}

            {section === "automation" && <Automation />}

            {section === "profiles" && <ProfilePanel />}

            {section === "memory" && <Memory workspaceId={workspace?.id} sessionId={sessionId} />}

            {section === "roster" && <AgentRoster />}

            {section === "agents" && (
              <>
                <h2>{t("skills", lang)}</h2>
                <p className="split-lead">Makinedeki skill’ler. Tehlikeli permission kırmızı kalır.</p>
                {skills.length === 0 ? (
                  <div className="empty">
                    <strong>Skill yok</strong>
                    <p>Sol sidebar’daki Marketplace’ten kur, ya da local path ver.</p>
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
                          {s.enabled ? "on" : t("disabled", lang)}
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
                <h2>MCP Servers</h2>
                <p className="split-lead">Model Context Protocol sunucuları. Command ile başlatılan araç sağlayıcıları.</p>
                {mcpServers.length === 0 ? (
                  <div className="empty">
                    <strong>MCP sunucu yok</strong>
                    <p>Aşağıdan bir MCP sunucusu ekle.</p>
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
                  <label>Name</label>
                  <input placeholder="my-mcp-server" value={mcpForm.name} onChange={(e) => setMcpForm((f) => ({ ...f, name: e.target.value }))} />
                  <label>Command</label>
                  <input placeholder="npx @modelcontextprotocol/server-filesystem" value={mcpForm.command} onChange={(e) => setMcpForm((f) => ({ ...f, command: e.target.value }))} />
                  <label>Args (space-separated)</label>
                  <input placeholder="/home/user/workspace" value={mcpForm.args} onChange={(e) => setMcpForm((f) => ({ ...f, args: e.target.value }))} />
                  <label>Env (KEY=VALUE, comma-separated)</label>
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
                    Add Server
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