import { rpc } from "./rpc";
import { loadAgents } from "./agents";
import { spaceOf } from "./spaces";
import type { AgentProfile, PersonaBadge, PersonaCatalog, Session } from "./types";

// The office used to be catalog characters picked in the side list, kept in
// this browser only. It is now the user's own agents (profiles), made in
// Settings → Office. moveOffice turns the old picks — and the characters
// office chats were held with — into agents once, and moves those chats
// under them, so no conversation goes missing. It reads everything itself:
// lists still loading elsewhere must not make it create an agent twice or
// miss a chat.
const DONE_KEY = "rove.office.v2";

export function officeMoved(): boolean {
  try { return localStorage.getItem(DONE_KEY) === "1"; } catch { return true; }
}

let running: Promise<boolean> | null = null;

export function moveOffice(): Promise<boolean> {
  if (officeMoved()) return Promise.resolve(false);
  running ??= move().finally(() => { running = null; });
  return running;
}

async function move(): Promise<boolean> {
  try {
    const [catalog, profiles, sessions, badges] = await Promise.all([
      rpc<PersonaCatalog>("persona.catalog"),
      rpc<AgentProfile[]>("profile.list"),
      rpc<Session[]>("session.list", { workspaceId: "" }),
      rpc<{ sessions: Record<string, PersonaBadge> }>("persona.badges"),
    ]);
    const badge = (id: string) => badges?.sessions?.[id] ?? null;
    const office = (sessions ?? []).filter((s) => spaceOf(s, badge(s.id)) === "office");
    const legacy = new Set(loadAgents());
    for (const s of office) {
      const b = badge(s.id);
      if (b?.characterId && !b.profileId) legacy.add(b.characterId);
    }
    let changed = false;
    const mine = [...(profiles ?? [])];
    for (const cid of legacy) {
      const c = catalog?.characters.find((x) => x.id === cid);
      if (!c) continue;
      let p = mine.find((x) => x.characterId === cid && x.name === c.name);
      if (!p) {
        p = await rpc<AgentProfile>("profile.upsert", { name: c.name, characterId: c.id, role: c.role });
        mine.push(p);
      }
      for (const s of office) {
        const b = badge(s.id);
        if (b?.characterId === cid && !b.profileId) await rpc("persona.set", { sessionId: s.id, profileId: p.id });
      }
      changed = true;
    }
    localStorage.setItem(DONE_KEY, "1");
    if (changed) {
      window.dispatchEvent(new Event("rove:profiles"));
      window.dispatchEvent(new Event("rove:persona"));
    }
    return changed;
  } catch {
    return false; // tried again next time the office opens
  }
}
