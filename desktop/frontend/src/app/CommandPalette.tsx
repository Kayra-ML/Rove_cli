import { useEffect, useMemo, useState } from "react";
import { filterSlash, SLASH, type SlashCmd } from "~/lib/slash";
import { t } from "~/lib/i18n";
import { usePrefs } from "~/hooks/usePrefs";

export type PaletteAction = {
  id: string;
  label: string;
  hint?: string;
  run: () => void;
};

interface Props {
  open: boolean;
  onClose: () => void;
  actions: PaletteAction[];
  onSlash?: (cmd: SlashCmd) => void;
}

export function CommandPalette({ open, onClose, actions, onSlash }: Props) {
  const { lang } = usePrefs();
  const [q, setQ] = useState("");
  const [hi, setHi] = useState(0);

  useEffect(() => {
    if (open) {
      setQ("");
      setHi(0);
    }
  }, [open]);

  const slashHits = q.startsWith("/") ? filterSlash(q) : [];
  const hits = useMemo(() => {
    if (q.startsWith("/")) return [];
    const n = q.trim().toLowerCase();
    if (!n) return actions;
    return actions.filter((a) => `${a.id} ${a.label} ${a.hint ?? ""}`.toLowerCase().includes(n));
  }, [q, actions]);

  const rows: { key: string; label: string; hint?: string; run: () => void }[] = slashHits.length
    ? slashHits.map((c) => ({
      key: c.id,
      label: `/${c.id}`,
      hint: c.hint,
      run: () => { onSlash?.(c); onClose(); },
    }))
    : hits.map((a) => ({
      key: a.id,
      label: a.label,
      hint: a.hint,
      run: () => { a.run(); onClose(); },
    }));

  if (!open) return null;

  return (
    <div className="palette-scrim" onClick={onClose}>
      <div className="palette" onClick={(e) => e.stopPropagation()}>
        <input
          autoFocus
          className="palette-input"
          placeholder={t("commandPalette", lang)}
          value={q}
          onChange={(e) => { setQ(e.target.value); setHi(0); }}
          onKeyDown={(e) => {
            if (e.key === "Escape") { e.preventDefault(); onClose(); return; }
            if (e.key === "ArrowDown") { e.preventDefault(); setHi((i) => (i + 1) % Math.max(rows.length, 1)); return; }
            if (e.key === "ArrowUp") { e.preventDefault(); setHi((i) => (i - 1 + Math.max(rows.length, 1)) % Math.max(rows.length, 1)); return; }
            if (e.key === "Enter") {
              e.preventDefault();
              rows[hi]?.run();
            }
          }}
        />
        <div className="palette-list">
          {rows.length === 0 ? (
            <div className="palette-empty">{t("noneYet", lang)}</div>
          ) : rows.map((r, i) => (
            <button
              key={r.key}
              className={`palette-row${i === hi ? " active" : ""}`}
              onMouseEnter={() => setHi(i)}
              onClick={() => r.run()}
            >
              <span>{r.label}</span>
              {r.hint && <em>{r.hint}</em>}
            </button>
          ))}
        </div>
        <div className="palette-foot">
          {SLASH.slice(0, 4).map((c) => `/${c.id}`).join("  ")}
        </div>
      </div>
    </div>
  );
}
