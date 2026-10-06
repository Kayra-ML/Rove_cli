import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { AgentProfile, Character, PersonaCatalog, Session } from "~/lib/types";
import { useProfiles } from "~/hooks/useApi";
import { usePersonaBadges, usePersonaCatalog } from "~/hooks/usePersona";
import { usePrefs } from "~/hooks/usePrefs";
import { FACES, MARK_COLORS, SHAPES, markOf, markString, type Mark } from "~/lib/agentmark";
import { filterModels, type ModelChoice } from "~/lib/slash";
import { t, type Lang } from "~/lib/i18n";
import { toast } from "~/lib/toast";
import { AgentMark, MarkSvg } from "./AgentMark";
import { CharacterGrid, FeatureToggles } from "./PersonaSheet";
import { Icon } from "./Icons";

// What Settings → Office opens with: the list, a new agent, or one agent.
export type OfficeIntent = { mode: "list" } | { mode: "new" } | { mode: "edit"; id: string };

type Draft = Partial<AgentProfile> & { name: string };
type PromptKind = "character" | "extra" | "own";

// promptKind reads what the saved fields mean: the character's prompt as
// is, the character's with additions, or a prompt of the user's own.
function promptKind(d: Draft): PromptKind {
  if (!d.characterId) return "own";
  if (d.promptMode === "own") return "own";
  return d.systemPrompt?.trim() ? "extra" : "character";
}

// OfficePanel is Settings → Office: the agents of the user's office. Only
// these appear when a chat is opened in Office; each one has its own logo,
// a starting character (or none), a system prompt, a model from the models
// the user has added, and its permissions — the defaults or its own.
export function OfficePanel({ intent }: { intent?: OfficeIntent }) {
  const { lang } = usePrefs();
  const [sessions, setSessions] = useState<Session[]>([]);
  useEffect(() => { rpc<Session[]>("session.list", { workspaceId: "" }).then((l) => setSessions(l ?? [])).catch(() => {}); }, []);
  const { profiles, reload } = useProfiles();
  const catalog = usePersonaCatalog();
  const { badgeFor } = usePersonaBadges();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [armed, setArmed] = useState<string | null>(null);

  useEffect(() => {
    if (!intent || intent.mode === "list") return;
    if (intent.mode === "new") setDraft({ name: "" });
    else {
      const p = profiles.find((x) => x.id === intent.id);
      if (p) setDraft({ ...p });
    }
  }, [intent, profiles]);

  const charOf = useCallback((id?: string) => catalog?.characters.find((c) => c.id === id), [catalog]);
  const chatsOf = (id: string) => sessions.filter((s) => badgeFor(s.id)?.profileId === id);

  const remove = async (p: AgentProfile) => {
    if (armed !== p.id) { setArmed(p.id); return; }
    setArmed(null);
    try {
      for (const s of chatsOf(p.id)) await rpc("session.delete", { id: s.id });
      await rpc("profile.delete", { id: p.id });
      await reload();
      window.dispatchEvent(new Event("rove:profiles"));
      window.dispatchEvent(new Event("rove:sessions"));
      window.dispatchEvent(new Event("rove:persona"));
    } catch (e) {
      toast(e instanceof Error ? e.message : "delete failed", "err");
    }
  };

  if (draft) {
    return (
      <AgentEditor
        lang={lang}
        draft={draft}
        cat={catalog}
        onCancel={() => setDraft(null)}
        onSaved={async () => { setDraft(null); await reload(); window.dispatchEvent(new Event("rove:profiles")); window.dispatchEvent(new Event("rove:persona")); }}
      />
    );
  }

  return (
    <div className="office">
      <div className="office-head">
        <div>
          <h2>{t("ofTitle", lang)}</h2>
          <p className="split-lead">{t("ofLead", lang)}</p>
        </div>
        <button type="button" className="primary" onClick={() => setDraft({ name: "" })}><Icon name="plus" size={13} /> {t("ofNew", lang)}</button>
      </div>
      {profiles.length === 0 ? (
        <div className="office-empty">
          <div className="office-empty-marks">
            {SHAPES.slice(0, 5).map((s, i) => <MarkSvg key={s} m={{ shape: s, face: FACES[i], color: MARK_COLORS[i * 2] }} size={30} />)}
          </div>
          <strong>{t("ofEmpty", lang)}</strong>
          <p>{t("ofEmptyHint", lang)}</p>
          <button type="button" className="primary" onClick={() => setDraft({ name: "" })}>{t("ofNew", lang)}</button>
        </div>
      ) : (
        <div className="office-list">
          {profiles.map((p) => {
            const c = charOf(p.characterId);
            const n = chatsOf(p.id).length;
            return (
              <div key={p.id} className="office-row">
                <AgentMark mark={p.mark} color={p.color} seed={p.id} character={p.characterId} size={34} />
                <button type="button" className="office-row-main" onClick={() => setDraft({ ...p })}>
                  <strong>{p.name}</strong>
                  <span className="office-row-meta">
                    <span>{c ? c.name : t("ofOwnAgent", lang)}</span>
                    <span>·</span>
                    <span className="office-model">{p.model ? `${p.model}${p.provider ? ` · ${p.provider}` : ""}` : t("ofModelChat", lang)}</span>
                    <span>·</span>
                    <span>{p.features ? `${p.features.length} ${t("ofPermsOwn", lang)}` : t("ofPermsDefault", lang)}</span>
                    {n > 0 && <><span>·</span><span>{n} {t("ofChats", lang)}</span></>}
                  </span>
                </button>
                <button type="button" className="ghost" onClick={() => setDraft({ ...p })}>{t("ofEdit", lang)}</button>
                <button
                  type="button"
                  className={`ghost danger${armed === p.id ? " armed" : ""}`}
                  onBlur={() => setArmed(null)}
                  onClick={() => void remove(p)}
                  title={n > 0 ? t("ofDeleteChats", lang).replace("{n}", String(n)) : undefined}
                >
                  {armed === p.id ? (n > 0 ? t("ofDeleteSure", lang).replace("{n}", String(n)) : t("ofDeleteSure0", lang)) : t("delete", lang)}
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function AgentEditor({ lang, draft: start, cat, onCancel, onSaved }: {
  lang: Lang;
  draft: Draft;
  cat: PersonaCatalog | null;
  onCancel: () => void;
  onSaved: () => void | Promise<void>;
}) {
  const catalog = cat?.characters ?? [];
  const features = cat?.features ?? [];
  const defaults = cat?.defaults ?? [];
  const [d, setD] = useState<Draft>(start);
  const [kind, setKind] = useState<PromptKind>(promptKind(start));
  const [models, setModels] = useState<ModelChoice[]>([]);
  const [mq, setMq] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [showPrompt, setShowPrompt] = useState(false);
  const seed = d.id ?? (d.name || "new");
  const mark = markOf(d.mark, d.color, seed, d.characterId);
  const char = catalog.find((c) => c.id === d.characterId);
  const set = (patch: Partial<Draft>) => setD((x) => ({ ...x, ...patch }));
  // the MCP servers it can be given (Settings → MCP)
  const [servers, setServers] = useState<{ id: string; name: string }[]>([]);
  useEffect(() => {
    let live = true;
    rpc<{ id: string; name: string }[]>("mcp.list", {}).then((l) => { if (live) setServers(Array.isArray(l) ? l : []); }).catch(() => {});
    return () => { live = false; };
  }, []);
  const setMark = (m: Mark) => set({ mark: markString(m), color: m.color });

  useEffect(() => { rpc<ModelChoice[]>("model.list").then((l) => setModels(l ?? [])).catch(() => setModels([])); }, []);
  // the picked model is in view when the list opens
  useEffect(() => {
    if (models.length === 0) return;
    // only the list scrolls, not the settings page around it
    const list = document.querySelector<HTMLElement>(".office-model-list");
    const on = list?.querySelector<HTMLElement>("button.on");
    if (list && on) list.scrollTop = on.offsetTop - list.clientHeight / 3;
  }, [models]);
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === "Escape") onCancel(); };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [onCancel]);

  const shown = useMemo(() => filterModels(models, mq), [models, mq]);
  const groups = useMemo(() => {
    const g = new Map<string, ModelChoice[]>();
    for (const m of shown) g.set(m.provider, [...(g.get(m.provider) ?? []), m]);
    return [...g.entries()];
  }, [shown]);

  const pickCharacter = (c: Character | null) => {
    set({ characterId: c?.id ?? "", role: c?.role ?? d.role, features: null, name: d.name.trim() ? d.name : c?.name ?? "" });
    if (!c) setKind("own");
    else if (kind === "own" && !d.systemPrompt?.trim()) setKind("character");
  };

  const save = async () => {
    if (!d.name.trim()) { setErr(t("ofNameNeeded", lang)); return; }
    setBusy(true);
    setErr("");
    const out: Partial<AgentProfile> = {
      ...d,
      name: d.name.trim(),
      mark: markString(mark),
      color: mark.color,
      promptMode: d.characterId && kind === "own" ? "own" : "",
      systemPrompt: kind === "character" ? "" : (d.systemPrompt ?? ""),
      role: d.role || char?.role || "developer",
    };
    try {
      await rpc("profile.upsert", out);
      toast(`${out.name}: ${t("ofSaved", lang)}`, "ok");
      await onSaved();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const effective = d.features ?? char?.features ?? defaults;

  return (
    <div className="office office-editor">
      <div className="office-head">
        <button type="button" className="ghost" onClick={onCancel}>← {t("ofBack", lang)}</button>
        <span className="tw-spacer" />
        {err && <span className="prov-err">{err}</span>}
        <button type="button" className="ghost" onClick={onCancel}>{t("cancel", lang)}</button>
        <button type="button" className="primary" disabled={busy} onClick={() => void save()}>{busy ? "…" : d.id ? t("ofSave", lang) : t("ofCreate", lang)}</button>
      </div>

      <div className="office-edit-grid">
        {/* the agent as the office will show it */}
        <aside className="office-preview">
          <div className="office-card">
            <MarkSvg m={mark} size={64} />
            <strong>{d.name.trim() || t("ofUnnamed", lang)}</strong>
            <span>{char ? char.name : t("ofOwnAgent", lang)}</span>
            <span className="office-model">{d.model ? `${d.model}${d.provider ? ` · ${d.provider}` : ""}` : t("ofModelChat", lang)}</span>
          </div>
          <div className="office-mark-pick">
            <div className="section-label">{t("ofShape", lang)}</div>
            <div className="office-swatches">
              {SHAPES.map((s) => (
                <button key={s} type="button" aria-pressed={mark.shape === s} className={mark.shape === s ? "on" : ""} onClick={() => setMark({ ...mark, shape: s })}>
                  <MarkSvg m={{ ...mark, shape: s }} size={26} />
                </button>
              ))}
            </div>
            <div className="section-label">{t("ofFace", lang)}</div>
            <div className="office-swatches">
              {FACES.map((f) => (
                <button key={f} type="button" aria-pressed={mark.face === f} className={mark.face === f ? "on" : ""} onClick={() => setMark({ ...mark, face: f })}>
                  <MarkSvg m={{ ...mark, face: f }} size={26} />
                </button>
              ))}
            </div>
            <div className="section-label">{t("ofColor", lang)}</div>
            <div className="office-swatches colors">
              {MARK_COLORS.map((c) => (
                <button key={c} type="button" aria-label={c} aria-pressed={mark.color === c} className={mark.color === c ? "on" : ""} onClick={() => setMark({ ...mark, color: c })}>
                  <span style={{ background: c }} />
                </button>
              ))}
            </div>
          </div>
        </aside>

        <div className="office-form">
          <section>
            <label className="office-label" htmlFor="of-name">{t("ofName", lang)}</label>
            <input id="of-name" autoFocus value={d.name} placeholder={t("ofNamePh", lang)} onChange={(e) => set({ name: e.target.value })} />
          </section>

          <section>
            <label className="office-label" htmlFor="of-title">{t("ofJobTitle", lang)}</label>
            <p className="office-hint">{t("ofJobTitleHint", lang)}</p>
            <input id="of-title" value={d.title ?? ""} placeholder={t("ofJobTitlePh", lang)} onChange={(e) => set({ title: e.target.value })} />
          </section>

          <section>
            <div className="office-label">{t("ofStart", lang)}</div>
            <p className="office-hint">{t("ofStartHint", lang)}</p>
            <CharacterGrid
              catalog={cat}
              compact
              selected={d.characterId || undefined}
              onPick={(c) => pickCharacter(c)}
              onNone={() => pickCharacter(null)}
            />
          </section>

          <section>
            <div className="office-label">{t("ofPrompt", lang)}</div>
            {char && (
              <div className="seg office-seg" role="radiogroup">
                {(["character", "extra", "own"] as const).map((k) => (
                  <button key={k} type="button" role="radio" aria-checked={kind === k} className={kind === k ? "on" : ""} onClick={() => setKind(k)}>
                    {t(`ofPrompt_${k}`, lang)}
                  </button>
                ))}
              </div>
            )}
            <p className="office-hint">{t(char ? `ofPromptHint_${kind}` : "ofPromptHint_blank", lang).replace("{c}", char?.name ?? "")}</p>
            {char && kind !== "own" && (
              <div className="office-base">
                <button type="button" className="ghost" onClick={() => setShowPrompt((v) => !v)}>{showPrompt ? "▾" : "▸"} {t("ofBasePrompt", lang).replace("{c}", char.name)}</button>
                {showPrompt && <pre>{char.prompt}</pre>}
              </div>
            )}
            {kind !== "character" && (
              <textarea
                rows={kind === "extra" ? 4 : 9}
                value={d.systemPrompt ?? ""}
                placeholder={t(kind === "extra" ? "ofExtraPh" : "ofOwnPh", lang)}
                onChange={(e) => set({ systemPrompt: e.target.value })}
              />
            )}
          </section>

          <section>
            <div className="office-label">{t("ofModel", lang)}</div>
            <p className="office-hint">{t("ofModelHint", lang)}</p>
            <div className="office-models">
              <input className="office-model-search" value={mq} placeholder={t("ofModelSearch", lang)} onChange={(e) => setMq(e.target.value)} />
              <div className="office-model-list" role="listbox" aria-label={t("ofModel", lang)}>
                {!mq.trim() && (
                  <button type="button" role="option" aria-selected={!d.model} className={!d.model ? "on" : ""} onClick={() => set({ model: "", provider: "" })}>
                    <span>{t("ofModelChat", lang)}</span>
                    <em>{t("ofModelChatHint", lang)}</em>
                  </button>
                )}
                {groups.map(([prov, list]) => (
                  <div key={prov} className="office-model-group">
                    <div className="model-group">{prov}</div>
                    {list.map((m) => {
                      const on = d.model === m.model && (d.provider ?? "") === m.provider;
                      return (
                        <button key={`${prov}/${m.model}`} type="button" role="option" aria-selected={on} className={on ? "on" : ""} onClick={() => set({ model: m.model, provider: m.provider })}>
                          <code>{m.model}</code>
                          {on && <Icon name="check" size={12} />}
                        </button>
                      );
                    })}
                  </div>
                ))}
                {models.length === 0 && <p className="map-muted">{t("ofNoModels", lang)}</p>}
              </div>
            </div>
          </section>

          <section>
            <div className="office-label">{t("ofPerms", lang)}</div>
            <div className="seg office-seg" role="radiogroup">
              <button type="button" role="radio" aria-checked={d.features == null} className={d.features == null ? "on" : ""} onClick={() => set({ features: null })}>{t("ofPermsDefault", lang)}</button>
              <button type="button" role="radio" aria-checked={d.features != null} className={d.features != null ? "on" : ""} onClick={() => set({ features: [...effective] })}>{t("ofPermsCustom", lang)}</button>
            </div>
            <p className="office-hint">{t(d.features == null ? "ofPermsDefaultHint" : "ofPermsCustomHint", lang).replace("{c}", char?.name ?? t("ofOwnAgent", lang))}</p>
            {d.features == null ? (
              <div className="office-perm-chips">
                {features.filter((f) => effective.includes(f.key)).map((f) => <span key={f.key} className="tw-chip" title={f.desc}>{f.name}</span>)}
              </div>
            ) : (
              <FeatureToggles features={features} value={d.features} onChange={(next) => set({ features: next })} />
            )}
            <p className="office-hint subtle">{t("ofPermsCli", lang)}</p>
          </section>

          <section>
            <div className="office-label">{t("ofIntegrations", lang)}</div>
            {servers.length === 0 ? (
              <p className="office-hint">{t("ofIntegrationsNone", lang)}</p>
            ) : (
              <>
                <div className="seg office-seg" role="radiogroup" aria-label={t("ofIntegrations", lang)}>
                  <button type="button" role="radio" aria-checked={d.integrations == null} className={d.integrations == null ? "on" : ""} onClick={() => set({ integrations: null })}>{t("ofIntegrationsAll", lang)}</button>
                  <button type="button" role="radio" aria-checked={d.integrations != null} className={d.integrations != null ? "on" : ""} onClick={() => set({ integrations: [] })}>{t("ofIntegrationsPick", lang)}</button>
                </div>
                <p className="office-hint">{t(d.integrations == null ? "ofIntegrationsAllHint" : "ofIntegrationsPickHint", lang)}</p>
                {d.integrations != null && (
                  <div className="office-integrations">
                    {servers.map((srv) => {
                      const on = d.integrations!.includes(srv.name);
                      return (
                        <label key={srv.id} className={`office-integration${on ? " on" : ""}`}>
                          <input type="checkbox" checked={on} onChange={() => set({ integrations: on ? d.integrations!.filter((x) => x !== srv.name) : [...d.integrations!, srv.name] })} />
                          <span>{srv.name}</span>
                        </label>
                      );
                    })}
                  </div>
                )}
              </>
            )}
          </section>

          <section>
            <label className="office-check">
              <input type="checkbox" checked={Boolean(d.isDefault)} onChange={(e) => set({ isDefault: e.target.checked })} />
              <span><strong>{t("ofDefault", lang)}</strong> — {t("ofDefaultHint", lang)}</span>
            </label>
          </section>
        </div>
      </div>
    </div>
  );
}
