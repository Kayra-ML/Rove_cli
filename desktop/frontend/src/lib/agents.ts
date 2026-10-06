import type { Character, PersonaBadge, Session } from "./types";

// "My agents": the catalog characters the user picked for the sidebar's
// Agents tab. Only the choice lives here; each agent's chats are found by
// the character their session runs as.
const KEY = "aether.agents";
export const AGENTS_EVENT = "rove:agents";

export function loadAgents(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || "[]") as unknown;
    return Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

export function saveAgents(ids: string[]) {
  try { localStorage.setItem(KEY, JSON.stringify(ids)); } catch { /* private */ }
  window.dispatchEvent(new Event(AGENTS_EVENT));
}

// chatsByAgent groups sessions (most recent first, as listed) under the
// character each one runs as.
export function chatsByAgent(sessions: Session[], badgeFor: (id: string) => PersonaBadge | null): Map<string, Session[]> {
  const out = new Map<string, Session[]>();
  for (const s of sessions) {
    const c = badgeFor(s.id)?.characterId;
    if (!c) continue;
    const list = out.get(c);
    if (list) list.push(s);
    else out.set(c, [s]);
  }
  return out;
}

// agentList is what the tab shows: the picked agents in pick order, then any
// other character that already has chats, so no history is ever hidden.
export function agentList(picked: string[], chats: Map<string, Session[]>, catalog: Character[]): Character[] {
  const byId = new Map(catalog.map((c) => [c.id, c]));
  const ids = [...picked, ...[...chats.keys()].filter((id) => !picked.includes(id))];
  return [...new Set(ids)].map((id) => byId.get(id)).filter((c): c is Character => Boolean(c));
}

// matchCharacter finds a catalog character from what a user typed after
// /team: its id, its name, or the start of either, or one of its tags.
export function matchCharacter(catalog: Character[], q: string): Character | null {
  const n = q.trim().toLocaleLowerCase("tr");
  if (!n) return null;
  const low = (x: string) => x.toLocaleLowerCase("tr");
  return catalog.find((c) => c.id === n || low(c.name) === n)
    ?? catalog.find((c) => c.id.startsWith(n) || low(c.name).startsWith(n))
    ?? catalog.find((c) => (c.tags ?? []).some((t) => low(t) === n))
    ?? catalog.find((c) => low(c.name).includes(n))
    ?? null;
}

// chatsByProfile groups office chats (most recent first, as listed) under
// the office agent — the profile — each one talks to.
export function chatsByProfile(sessions: Session[], badgeFor: (id: string) => PersonaBadge | null): Map<string, Session[]> {
  const out = new Map<string, Session[]>();
  for (const s of sessions) {
    const p = badgeFor(s.id)?.profileId;
    if (!p) continue;
    const list = out.get(p);
    if (list) list.push(s);
    else out.set(p, [s]);
  }
  return out;
}
