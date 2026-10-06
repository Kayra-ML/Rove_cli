import { useMemo, useState } from "react";
import type { Character, Feature, PersonaCatalog } from "~/lib/types";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { Avatar } from "./Avatar";

// Character and feature pickers, used by Settings → Profiles.
export function CharacterGrid({
  catalog, selected, onPick, onNone, compact,
}: {
  catalog: PersonaCatalog | null;
  selected?: string;
  onPick: (c: Character) => void;
  onNone?: () => void;
  compact?: boolean;
}) {
  const { lang } = usePrefs();
  const [q, setQ] = useState("");
  const [cat, setCat] = useState<string>("");
  const [peek, setPeek] = useState<string | null>(null);
  const cats = useMemo(() => [...new Set((catalog?.characters ?? []).map((c) => c.category))], [catalog]);
  const list = useMemo(() => {
    const needle = q.trim().toLocaleLowerCase("tr");
    return (catalog?.characters ?? []).filter((c) => {
      if (cat && c.category !== cat) return false;
      if (!needle) return true;
      return [c.name, c.summary, c.id, ...(c.tags ?? [])].some((s) => s.toLocaleLowerCase("tr").includes(needle));
    });
  }, [catalog, q, cat]);
  if (!catalog) return <div className="map-muted">…</div>;
  return (
    <div className="char-grid-wrap">
      <div className="char-filter">
        <input value={q} placeholder={t("personaSearch", lang)} onChange={(e) => setQ(e.target.value)} autoFocus={!compact} />
        <div className="cmap-seg">
          <button type="button" className={!cat ? "on" : ""} onClick={() => setCat("")}>{t("personaAll", lang)}</button>
          {cats.map((c) => (
            <button type="button" key={c} className={cat === c ? "on" : ""} onClick={() => setCat(c)}>{c}</button>
          ))}
        </div>
      </div>
      <div className={`char-grid${compact ? " compact" : ""}`}>
        {onNone && (
          <button type="button" className={`persona-card${!selected ? " on" : ""}`} onClick={onNone}>
            <Avatar size="md" />
            <span className="persona-card-text">
              <strong>{t("personaNone", lang)}</strong>
              <span>{t("personaNoneHint", lang)}</span>
            </span>
          </button>
        )}
        {list.map((c) => (
          <div key={c.id} className={`persona-card${selected === c.id ? " on" : ""}`} role="button" tabIndex={0}
            onClick={() => onPick(c)}
            onKeyDown={(e) => { if (e.key === "Enter") onPick(c); }}
          >
            <Avatar name={c.name} size="md" />
            <span className="persona-card-text">
              <strong>{c.name}</strong>
              <span>{c.summary}</span>
              {!compact && (
                <span className="char-tags">
                  {(c.tags ?? []).slice(0, 4).map((tg) => <i key={tg}>{tg}</i>)}
                  <button type="button" className="char-peek" onClick={(e) => { e.stopPropagation(); setPeek(peek === c.id ? null : c.id); }}>
                    {peek === c.id ? t("personaHidePrompt", lang) : t("personaShowPrompt", lang)}
                  </button>
                </span>
              )}
              {peek === c.id && <pre className="char-prompt">{c.prompt}</pre>}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function FeatureToggles({
  features, value, onChange,
}: {
  features: Feature[];
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const { lang } = usePrefs();
  const on = new Set(value);
  const toggle = (k: string) => {
    const next = new Set(on);
    if (next.has(k)) next.delete(k); else next.add(k);
    onChange(features.map((f) => f.key).filter((x) => next.has(x)));
  };
  return (
    <div className="feat-groups">
      {(["tools", "behavior"] as const).map((g) => (
        <div key={g}>
          <div className="section-label feat-label">{t(g === "tools" ? "personaTools" : "personaBehavior", lang)}</div>
          <div className="feat-grid">
            {features.filter((f) => f.group === g).map((f) => (
              <label key={f.key} className={`feat${on.has(f.key) ? " on" : ""}`} title={f.rule ?? (f.tools ?? []).join(", ")}>
                <input type="checkbox" checked={on.has(f.key)} onChange={() => toggle(f.key)} />
                <span>
                  <strong>{f.name}</strong>
                  <span>{f.desc}</span>
                </span>
              </label>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
