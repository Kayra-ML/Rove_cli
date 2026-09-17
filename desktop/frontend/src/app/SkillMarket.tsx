import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { AutomationJob, AutomationTemplate, InstalledSkill, MarketSkill } from "~/lib/types";
import { useSkills } from "~/hooks/useApi";
import { Icon } from "./Icons";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

type Kind = "plugins" | "skills" | "automations";

function permBits(p?: InstalledSkill["manifest"]["permissions"]) {
  if (!p) return [] as string[];
  return [
    p.shell && "shell",
    p.network && "net",
    p.filesystem && "fs",
    p.git && "git",
  ].filter(Boolean) as string[];
}

function isPlugin(s: MarketSkill) {
  return s.kind === "plugin";
}

function humanSeconds(s: number) {
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}m`;
  if (s < 86400) return `${Math.round(s / 3600)}h`;
  return `${Math.round(s / 86400)}d`;
}

export function SkillMarket() {
  const { skills } = useSkills();
  const { lang } = usePrefs();
  const [kind, setKind] = useState<Kind>("plugins");
  const [query, setQuery] = useState("");
  const [catalog, setCatalog] = useState<MarketSkill[]>([]);
  const [searching, setSearching] = useState(false);

  // --- Automation state ---
  const [automCatalog, setAutomCatalog] = useState<AutomationTemplate[]>([]);
  const [automJobs, setAutomJobs] = useState<AutomationJob[]>([]);
  const [automBusy, setAutomBusy] = useState<Record<string, boolean>>({});
  const [automLoading, setAutomLoading] = useState(false);

  const loadAutomations = useCallback(async () => {
    setAutomLoading(true);
    try {
      const [tmplRes, jobsRes] = await Promise.all([
        rpc<AutomationTemplate[]>("automation.catalog", {}),
        rpc<AutomationJob[]>("automation.list", {}),
      ]);
      setAutomCatalog(tmplRes ?? []);
      setAutomJobs(jobsRes ?? []);
    } finally {
      setAutomLoading(false);
    }
  }, []);

  useEffect(() => {
    if (kind === "automations") void loadAutomations();
  }, [kind, loadAutomations]);

  const handleInstall = useCallback(async (name: string) => {
    setAutomBusy((b) => ({ ...b, [name]: true }));
    try {
      await rpc("automation.install", { name });
      await loadAutomations();
    } finally {
      setAutomBusy((b) => ({ ...b, [name]: false }));
    }
  }, [loadAutomations]);

  const handleUninstall = useCallback(async (name: string) => {
    setAutomBusy((b) => ({ ...b, [name]: true }));
    try {
      await rpc("automation.uninstall", { name });
      await loadAutomations();
    } finally {
      setAutomBusy((b) => ({ ...b, [name]: false }));
    }
  }, [loadAutomations]);

  const handleToggle = useCallback(async (job: AutomationJob) => {
    setAutomBusy((b) => ({ ...b, [job.name]: true }));
    try {
      await rpc("automation.upsert", { ...job, enabled: !job.enabled });
      await loadAutomations();
    } finally {
      setAutomBusy((b) => ({ ...b, [job.name]: false }));
    }
  }, [loadAutomations]);

  // --- Skill/plugin state ---
  const search = useCallback(async (q = query) => {
    setSearching(true);
    try {
      const results = await rpc<MarketSkill[]>("market.list", { q });
      setCatalog(results ?? []);
    } catch {
      setCatalog([]);
    } finally {
      setSearching(false);
    }
  }, [query]);

  useEffect(() => {
    void search("");
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const q = query.trim().toLowerCase();
  const installedFiltered = useMemo(() => {
    if (!q) return skills;
    return skills.filter((s) =>
      `${s.manifest.name} ${s.manifest.description}`.toLowerCase().includes(q),
    );
  }, [skills, q]);

  const plugins = useMemo(
    () => catalog.filter((s) => isPlugin(s) && (!q || `${s.manifest.name} ${s.manifest.description}`.toLowerCase().includes(q))),
    [catalog, q],
  );
  const marketSkills = useMemo(
    () => catalog.filter((s) => !isPlugin(s) && (!q || `${s.manifest.name} ${s.manifest.description} ${s.plugin ?? ""}`.toLowerCase().includes(q))),
    [catalog, q],
  );

  const skillGroups = useMemo(() => {
    const map = new Map<string, MarketSkill[]>();
    for (const s of marketSkills) {
      const key = s.plugin || "other";
      const arr = map.get(key) ?? [];
      arr.push(s);
      map.set(key, arr);
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [marketSkills]);

  // Installed jobs lookup by name
  const jobByName = useMemo(() => {
    const m = new Map<string, AutomationJob>();
    for (const j of automJobs) m.set(j.name.toLowerCase(), j);
    return m;
  }, [automJobs]);

  return (
    <div className="catalog">
      <header className="catalog-head">
        <div className="mode-switch">
          <button className={kind === "plugins" ? "active" : ""} onClick={() => setKind("plugins")}>
            <Icon name="box" size={13} /> {t("plugins", lang)}
          </button>
          <button className={kind === "skills" ? "active" : ""} onClick={() => setKind("skills")}>
            <Icon name="skills" size={13} /> {t("skills", lang)}
          </button>
          <button className={kind === "automations" ? "active" : ""} onClick={() => setKind("automations")}>
            <Icon name="lightning" size={13} /> {t("automations", lang)}
          </button>
        </div>
        {kind !== "automations" && (
          <div className="catalog-search">
            <Icon name="search" size={14} />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") void search(); }}
              placeholder={t("search", lang)}
            />
          </div>
        )}
      </header>

      <div className="catalog-body">
        {kind === "plugins" && (
          <>
            <div className="catalog-meta">
              RoveCode · {plugins.length}{searching ? " · searching" : ""}
              {installedFiltered.length > 0 ? ` · installed ${installedFiltered.length}` : ""}
            </div>
            {plugins.length === 0 && !searching ? (
              <div className="empty">
                <strong>No plugins</strong>
                <p>Catalog seeds from RoveCode_plugins on first boot.</p>
              </div>
            ) : (
              <div className="catalog-grid">
                {plugins.map((s: MarketSkill) => (
                  <article key={s.manifest.name} className="catalog-card">
                    <div className="catalog-card-icon"><Icon name="box" size={16} /></div>
                    <div className="catalog-card-body">
                      <h3>
                        {s.manifest.name}
                        <span>v{s.manifest.version}</span>
                      </h3>
                      <p>{s.manifest.description || "No description"}</p>
                      <div className="chip-row" style={{ margin: "8px 0 0" }}>
                        <span className="chip">rovecode</span>
                        {s.manifest.author && <span className="chip">{s.manifest.author}</span>}
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            )}
            {installedFiltered.length > 0 && (
              <div className="catalog-meta" style={{ marginTop: 18 }}>Installed · {installedFiltered.length}</div>
            )}
            {installedFiltered.length > 0 && (
              <div className="catalog-grid">
                {installedFiltered.map((s: InstalledSkill) => (
                  <article key={s.manifest.name} className="catalog-card">
                    <div className="catalog-card-icon"><Icon name="box" size={16} /></div>
                    <div className="catalog-card-body">
                      <h3>
                        {s.manifest.name}
                        <span>v{s.manifest.version}</span>
                      </h3>
                      <p>{s.manifest.description || "No description"}</p>
                      {permBits(s.manifest.permissions).length > 0 && (
                        <div className="chip-row" style={{ margin: "8px 0 0" }}>
                          {permBits(s.manifest.permissions).map((b) => (
                            <span key={b} className="chip" style={{ color: "var(--bad)" }}>{b}</span>
                          ))}
                        </div>
                      )}
                    </div>
                  </article>
                ))}
              </div>
            )}
          </>
        )}

        {kind === "skills" && (
          <>
            <div className="catalog-meta">
              Marketplace · {marketSkills.length}{searching ? " · searching" : ""}
            </div>
            {marketSkills.length === 0 && !searching ? (
              <div className="empty">
                <strong>Browse the catalog</strong>
                <p>Type a capability — rust, test, layout.</p>
              </div>
            ) : (
              skillGroups.map(([plugin, items]) => (
                <section key={plugin} className="catalog-group">
                  <div className="catalog-meta">{plugin} · {items.length}</div>
                  <div className="catalog-grid">
                    {items.map((s: MarketSkill) => (
                      <article key={s.manifest.name} className="catalog-card">
                        <div className="catalog-card-icon"><Icon name="skills" size={16} /></div>
                        <div className="catalog-card-body">
                          <h3>
                            {s.manifest.name}
                            <span>v{s.manifest.version}</span>
                            {s.signed && <span className="badge ok" style={{ marginLeft: 6 }}>signed</span>}
                          </h3>
                          <p>{s.manifest.description || "No description"}</p>
                        </div>
                      </article>
                    ))}
                  </div>
                </section>
              ))
            )}
          </>
        )}

        {kind === "automations" && (
          <>
            {/* ── Catalog ── */}
            <div className="catalog-meta" style={{ marginBottom: 6 }}>
              Katalog · {automCatalog.length}{automLoading ? " · yükleniyor" : ""}
            </div>
            {automCatalog.length === 0 && !automLoading ? (
              <div className="empty">
                <strong>Otomasyon bulunamadı</strong>
                <p>Bundled otomasyonlar yükleniyor...</p>
              </div>
            ) : (
              <div className="catalog-grid">
                {automCatalog.map((tmpl) => {
                  const job = jobByName.get(tmpl.name.toLowerCase());
                  const busy = automBusy[tmpl.name] ?? false;
                  return (
                    <article key={tmpl.name} className={`catalog-card autom-card${job ? " autom-installed" : ""}`}>
                      <div className="catalog-card-icon autom-icon">
                        <Icon name="lightning" size={16} />
                      </div>
                      <div className="catalog-card-body">
                        <h3>
                          {tmpl.name}
                          <span>v{tmpl.version}</span>
                          {job && (
                            <span className={`badge ${job.enabled ? "ok" : "muted"}`} style={{ marginLeft: 6 }}>
                              {job.enabled ? "aktif" : "durduruldu"}
                            </span>
                          )}
                        </h3>
                        <p>{tmpl.description}</p>
                        <div className="chip-row" style={{ margin: "6px 0 0" }}>
                          <span className="chip">her {humanSeconds(tmpl.everySeconds)}</span>
                          {tmpl.permissions.shell && <span className="chip chip-warn">shell</span>}
                          {tmpl.permissions.git && <span className="chip chip-warn">git</span>}
                          {tmpl.permissions.network && <span className="chip chip-warn">net</span>}
                          {tmpl.tags?.map((tag) => <span key={tag} className="chip">{tag}</span>)}
                        </div>
                      </div>
                      <div className="autom-actions">
                        {job ? (
                          <>
                            {/* toggle switch */}
                            <button
                              className={`autom-toggle${job.enabled ? " on" : ""}`}
                              title={job.enabled ? "Durdur" : "Başlat"}
                              disabled={busy}
                              onClick={() => handleToggle(job)}
                            >
                              <span className="autom-toggle-knob" />
                            </button>
                            {/* uninstall */}
                            <button
                              className="autom-remove"
                              title="Kaldır"
                              disabled={busy}
                              onClick={() => handleUninstall(tmpl.name)}
                            >
                              <Icon name="trash" size={13} />
                            </button>
                          </>
                        ) : (
                          <button
                            className="autom-install"
                            disabled={busy}
                            onClick={() => handleInstall(tmpl.name)}
                          >
                            {busy ? "…" : "Kur"}
                          </button>
                        )}
                      </div>
                    </article>
                  );
                })}
              </div>
            )}

            {/* ── Installed (kurulanlar) ── */}
            {automJobs.length > 0 && (
              <>
                <div className="catalog-meta" style={{ marginTop: 20, marginBottom: 6 }}>
                  Kurulan otomasyonlar · {automJobs.length}
                </div>
                <div className="autom-installed-list">
                  {automJobs.map((job) => {
                    const busy = automBusy[job.name] ?? false;
                    return (
                      <div key={job.id} className={`autom-row${job.enabled ? " enabled" : ""}`}>
                        <div className="autom-row-icon">
                          <Icon name="lightning" size={14} />
                        </div>
                        <div className="autom-row-info">
                          <span className="autom-row-name">{job.name}</span>
                          {job.lastResult && (
                            <span className="autom-row-last">{job.lastResult}</span>
                          )}
                        </div>
                        <span className="autom-row-interval">her {humanSeconds(job.everySeconds)}</span>
                        <button
                          className={`autom-toggle${job.enabled ? " on" : ""}`}
                          title={job.enabled ? "Durdur" : "Başlat"}
                          disabled={busy}
                          onClick={() => handleToggle(job)}
                        >
                          <span className="autom-toggle-knob" />
                        </button>
                        <button
                          className="autom-remove"
                          title="Kaldır"
                          disabled={busy}
                          onClick={() => handleUninstall(job.name)}
                        >
                          <Icon name="trash" size={13} />
                        </button>
                      </div>
                    );
                  })}
                </div>
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}