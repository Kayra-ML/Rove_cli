import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc } from "~/lib/rpc";
import { localizeCatalog } from "~/lib/catalogI18n";
import { usePrefs } from "./usePrefs";
import type { PersonaBadge, PersonaCatalog, PersonaView } from "~/lib/types";

// The catalog is static for a daemon's lifetime: fetch it once per page.
let catalogOnce: Promise<PersonaCatalog> | null = null;

function loadCatalog(): Promise<PersonaCatalog> {
  if (!catalogOnce) {
    catalogOnce = rpc<PersonaCatalog>("persona.catalog").catch((err) => {
      catalogOnce = null;
      throw err;
    });
  }
  return catalogOnce;
}

// usePersonaCatalog is the catalog in the app's current language.
export function usePersonaCatalog() {
  const { lang } = usePrefs();
  const [catalog, setCatalog] = useState<PersonaCatalog | null>(null);
  useEffect(() => {
    let live = true;
    loadCatalog().then((c) => live && setCatalog(c)).catch(() => {});
    return () => { live = false; };
  }, []);
  return useMemo(() => (catalog ? localizeCatalog(catalog, lang) : null), [catalog, lang]);
}

// useCharacterName shows a character by its id in the app's language; names
// that come from the daemon (team members, a chat's agent) are Turkish.
export function useCharacterName() {
  const catalog = usePersonaCatalog();
  return useCallback(
    (characterId: string | undefined, fallback = "") => catalog?.characters.find((c) => c.id === characterId)?.name ?? fallback,
    [catalog],
  );
}

export function usePersona(sessionId: string | null | undefined) {
  const [view, setView] = useState<PersonaView | null>(null);
  const reload = useCallback(async () => {
    if (!sessionId) { setView(null); return; }
    try { setView(await rpc<PersonaView>("persona.get", { sessionId })); } catch { setView(null); }
  }, [sessionId]);
  useEffect(() => {
    void reload();
    // a role picked elsewhere (/character in another terminal) shows here
    const on = () => void reload();
    window.addEventListener("rove:persona", on);
    return () => window.removeEventListener("rove:persona", on);
  }, [reload]);
  const set = useCallback(async (patch: Record<string, unknown>) => {
    if (!sessionId) return null;
    const v = await rpc<PersonaView>("persona.set", { sessionId, ...patch });
    setView(v);
    window.dispatchEvent(new Event("rove:persona"));
    return v;
  }, [sessionId]);
  const clear = useCallback(async () => {
    if (!sessionId) return null;
    const v = await rpc<PersonaView>("persona.clear", { sessionId });
    setView(v);
    window.dispatchEvent(new Event("rove:persona"));
    return v;
  }, [sessionId]);
  return { view, reload, set, clear };
}

// Badges for every session in one call; refreshed when any persona changes.
export function usePersonaBadges() {
  const [badges, setBadges] = useState<{ sessions: Record<string, PersonaBadge>; default: PersonaBadge | null }>({ sessions: {}, default: null });
  const reload = useCallback(async () => {
    try {
      const b = await rpc<{ sessions: Record<string, PersonaBadge>; default: PersonaBadge | null }>("persona.badges");
      setBadges({ sessions: b.sessions ?? {}, default: b.default ?? null });
    } catch { /* keep */ }
  }, []);
  useEffect(() => {
    void reload();
    const on = () => void reload();
    window.addEventListener("rove:persona", on);
    return () => window.removeEventListener("rove:persona", on);
  }, [reload]);
  const badgeFor = useCallback((sessionId: string): PersonaBadge | null => {
    const b = badges.sessions[sessionId];
    if (b) return b.name ? b : null;
    return badges.default;
  }, [badges]);
  return { badgeFor, reload };
}
