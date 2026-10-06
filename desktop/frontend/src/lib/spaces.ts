import type { PersonaBadge, Session } from "./types";

// The app is split into spaces: Office (talk to an agent), Chat (session
// work, with modes) and Automation (the maps). Market is a
// page of its own, reached from the sidebar.
export type Space = "office" | "chat";
export type Top = Space | "automation" | "market";
export const TOPS: Exclude<Top, "market">[] = ["office", "chat", "automation"];

// A chat in the Chat space is shown in one of these modes; each chat keeps
// its own. (Chat itself is the space, not a mode.)
export type ChatMode = "orchestra" | "teamwork" | "terminal";
export const CHAT_MODES: ChatMode[] = ["orchestra", "teamwork", "terminal"];
// a chat that never had a mode starts in the one used last, else Orchestra
const DEFAULT_MODE: ChatMode = "orchestra";

// spaceOf places a chat. Chats made before spaces existed have none: those
// that talk to a catalog character are Office, the rest Chat.
export function spaceOf(s: Session, badge: PersonaBadge | null): Space {
  if (s.space === "office" || s.space === "chat") return s.space;
  return badge?.characterId ? "office" : "chat";
}

const TOP_KEY = "aether.mode";

// loadTop reads the last space, mapping the old per-view modes onto the new
// spaces (agent/terminal/board were chat views; map/context are automation).
export function loadTop(): Top {
  let v: string | null = null;
  try { v = localStorage.getItem(TOP_KEY); } catch { /* private */ }
  switch (v) {
    case "office": case "chat": case "automation": case "market": return v;
    case "agent": case "terminal": case "board": return "chat";
    case "map": case "context": return "automation";
    default: return "office";
  }
}

export function saveTop(top: Top) {
  try { localStorage.setItem(TOP_KEY, top); } catch { /* private */ }
}

// Per-chat modes. A draft (no chat yet) keeps its choice under DRAFT and
// hands it to the chat its first message creates; LAST is the mode used most
// recently anywhere, which a chat without a mode of its own starts in.
const MODES_KEY = "aether.chat.modes";
export const DRAFT = "draft";
export const LAST = "last";

export function loadModes(): Record<string, ChatMode> {
  try {
    const raw = JSON.parse(localStorage.getItem(MODES_KEY) || "{}") as Record<string, unknown>;
    const out: Record<string, ChatMode> = {};
    for (const [k, v] of Object.entries(raw)) {
      // "chat" was a mode once (a plain conversation, which Orchestra now is);
      // a mode that no longer exists falls back to the default
      const m = v === "chat" ? "orchestra" : v;
      if (CHAT_MODES.includes(m as ChatMode)) out[k] = m as ChatMode;
    }
    return out;
  } catch {
    return {};
  }
}

export function saveModes(m: Record<string, ChatMode>) {
  try { localStorage.setItem(MODES_KEY, JSON.stringify(m)); } catch { /* private */ }
}

export function modeFor(modes: Record<string, ChatMode>, sessionId: string | null | undefined): ChatMode {
  return modes[sessionId ?? DRAFT] ?? modes[LAST] ?? DEFAULT_MODE;
}

// withMode records a chat's mode, and makes it the one new chats start in.
export function withMode(modes: Record<string, ChatMode>, sessionId: string | null | undefined, mode: ChatMode): Record<string, ChatMode> {
  return { ...modes, [sessionId ?? DRAFT]: mode, [LAST]: mode };
}

// freshDraft forgets a previous draft's choice: a new draft starts in LAST.
export function freshDraft(modes: Record<string, ChatMode>): Record<string, ChatMode> {
  if (!(DRAFT in modes)) return modes;
  const next = { ...modes };
  delete next[DRAFT];
  return next;
}

// adopt moves a draft's mode onto the chat created from it.
export function adopt(modes: Record<string, ChatMode>, sessionId: string): Record<string, ChatMode> {
  const mode = modes[DRAFT];
  const next = { ...modes };
  delete next[DRAFT];
  if (mode) next[sessionId] = mode;
  return next;
}
