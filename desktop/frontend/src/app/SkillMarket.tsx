import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { InstalledSkill, MarketSkill } from "~/lib/types";
import { useSkills } from "~/hooks/useApi";
import { Icon } from "./Icons";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

type Kind = "plugins" | "skills";

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

export function SkillMarket() {
  const { skills } = useSkills();
  const { lang } = usePrefs();
  const [kind, setKind] = useState<Kind>("plugins");
  const [query, setQuery] = useState("");
  const [catalog, setCatalog] = useState<MarketSkill[]>([]);
  const [searching, setSearching] = useState(false);

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
        </div>
        <div className="catalog-search">
          <Icon name="search" size={14} />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") void search(); }}
            placeholder={t("search", lang)}
          />
        </div>
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
      </div>
    </div>
  );
}
