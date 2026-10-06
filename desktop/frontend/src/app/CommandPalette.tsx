import { useEffect, useMemo, useState } from "react";
import { filterSlash, SLASH, type SlashCmd } from "~/lib/slash";
import { rpc } from "~/lib/rpc";
import type { Session } from "~/lib/types";
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
  onOpenSession?: (s: Session) => void;
}

// How many chats a search may offer before the list stops being a shortcut
// and starts being a second session sidebar.
const SESSION_HITS = 6;

export function CommandPalette({ open, onClose, actions, onSlash, onOpenSession }: Props) {
  const { lang } = usePrefs();
  const [q, setQ] = useState("");
  const [hi, setHi] = useState(0);
  const [sessions, setSessions] = useState<Session[]>([]);

  useEffect(() => {
    if (open) {
      setQ("");
      setHi(0);
    }
  }, [open]);

  // Chats are the other thing you come here to reach. The list is fetched
  // when the palette opens rather than held open all session; session.list
  // is a shared read, so opening it twice in a row costs one round trip.
  useEffect(() => {
    if (!open || !onOpenSession) return;
    let gone = false;
    void rpc<Session[]>("session.list")
      .then((list) => { if (!gone) setSessions(list ?? []); })
      .catch(() => { if (!gone) setSessions([]); });
    return () => { gone = true; };
  }, [open, onOpenSession]);

  const slashHits = q.startsWith("/") ? filterSlash(q) : [];
  const hits = useMemo(() => {
    if (q.startsWith("/")) return [];
    const n = q.trim().toLowerCase();
    if (!n) return actions;
    return actions.filter((a) => `${a.id} ${a.label} ${a.hint ?? ""}`.toLowerCase().includes(n));
  }, [q, actions]);

  // Chats join the list only once you have typed: with the box just opened
  // the commands are the whole point, and a chat list would bury them.
  const sessionHits = useMemo(() => {
    if (!onOpenSession || q.startsWith("/")) return [];
    const n = q.trim().toLowerCase();
    if (!n) return [];
    return sessions
      .filter((s) => (s.title || "").toLowerCase().includes(n))
      .slice(0, SESSION_HITS);
  }, [q, sessions, onOpenSession]);

  const rows: { key: string; label: string; hint?: string; run: () => void }[] = slashHits.length
    ? slashHits.map((c) => ({
      key: c.id,
      label: `/${c.id}`,
      hint: c.hint,
      run: () => { onSlash?.(c); onClose(); },
    }))
    : [
      ...hits.map((a) => ({
        key: a.id,
        label: a.label,
        hint: a.hint,
        run: () => { a.run(); onClose(); },
      })),
      ...sessionHits.map((s) => ({
        key: `session:${s.id}`,
        label: s.title || t("newSession", lang),
        hint: t("sessions", lang),
        run: () => { onOpenSession?.(s); onClose(); },
      })),
    ];

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
