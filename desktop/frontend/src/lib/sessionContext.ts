import type { Message } from "./types";
import { splitContext } from "./attachments";

// sessionContext reads a chat's history as the context the model is working
// with: every request you made, and what the agent touched to answer it —
// files it read or wrote, commands it ran, sessions it talked to. Nothing is
// asked of the daemon beyond the history itself.

export type CtxKind = "session" | "prompt" | "file" | "command" | "web" | "agent";

export type CtxItem = {
  id: string;
  kind: CtxKind;
  label: string;
  // the full text: a prompt's words, a file's path, a command line
  detail: string;
  // indexes into turns where this came up
  turns: number[];
  reads: number;
  writes: number;
  runs: number;
  errors: number;
  // the last thing a tool said about it, clipped
  last?: string;
};

export type CtxTurn = {
  index: number;
  prompt: string;
  // the last thing the agent said in this turn
  answer: string;
  // where the request sits in the history: keeping this many messages
  // forgets it and everything after
  msgIndex: number;
  at: string;
  // rough size of the turn: prompt, answers and tool output
  tokens: number;
  items: string[];
};

export type CtxEdge = { from: string; to: string; kind: "turn" | "read" | "write" | "run" | "uses" | "talk" };

export type SessionContext = {
  turns: CtxTurn[];
  items: Map<string, CtxItem>;
  edges: CtxEdge[];
  // everything ever said, and what the model still sees after the last
  // compaction summary
  tokens: number;
  liveTokens: number;
  // what the live part is made of
  parts: { user: number; assistant: number; tools: number; summary: number };
  messages: number;
  toolCalls: number;
  compacted: boolean;
};

type Raw = Message & {
  toolCalls?: { id: string; name: string; argsJson?: string }[] | null;
  toolResult?: { toolCallId: string; name: string; content?: string; isError?: boolean } | null;
};

// the usual ~4 characters per token
export const tokensOf = (s: string | undefined | null) => Math.ceil((s?.length ?? 0) / 4);

const FILE_READ = new Set(["read_file", "list_dir"]);
const FILE_WRITE = new Set(["write_file", "patch_file", "edit_file", "create_file"]);
const AGENT = new Set(["team_delegate", "message_session", "linked_sessions"]);

function parse(json?: string): Record<string, unknown> {
  if (!json) return {};
  try {
    const v = JSON.parse(json) as unknown;
    return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

const str = (v: unknown) => (typeof v === "string" ? v : "");
const base = (p: string) => p.replace(/\/+$/, "").split("/").pop() || p;
const clip = (s: string, n: number) => {
  const one = s.replace(/\s+/g, " ").trim();
  return one.length > n ? `${one.slice(0, n - 1)}…` : one;
};
// paths a command line names, so running a file links to that file
const PATH_RX = /(?:^|[\s"'=(])((?:~|\.{1,2})?\/[\w.\-/@+]+|[\w\-]+\/[\w.\-/@+]*\.\w+|[\w\-]+\.(?:py|js|ts|tsx|go|rs|sh|rb|json|md|txt|csv|html|css|yaml|yml|toml))/g;

// commandName is what a command line is called on the map: "cd /tmp &&"
// prefixes say where, not what, so they are dropped.
export function commandName(cmd: string): string {
  let c = cmd.trim();
  for (let i = 0; i < 4; i++) {
    const m = c.match(/^cd\s+[^&;|]+(?:&&|;)\s*/);
    if (!m) break;
    c = c.slice(m[0].length);
  }
  return c.split(/\s+/).slice(0, 3).join(" ") || cmd;
}

export function buildSessionContext(history: Message[], sessionId: string, title: string): SessionContext {
  const msgs = history as Raw[];
  const items = new Map<string, CtxItem>();
  const edges: CtxEdge[] = [];
  const seenEdge = new Set<string>();
  const turns: CtxTurn[] = [];
  const results = new Map<string, { content: string; isError: boolean }>();
  for (const m of msgs) {
    if (m.role === "tool" && m.toolResult?.toolCallId) {
      results.set(m.toolResult.toolCallId, { content: m.toolResult.content ?? m.content ?? "", isError: Boolean(m.toolResult.isError) });
    }
  }

  const root = `s:${sessionId}`;
  items.set(root, { id: root, kind: "session", label: title || "Sohbet", detail: title, turns: [], reads: 0, writes: 0, runs: 0, errors: 0 });
  const link = (from: string, to: string, kind: CtxEdge["kind"]) => {
    const k = `${from}>${to}`;
    if (from === to || seenEdge.has(k)) return;
    seenEdge.add(k);
    edges.push({ from, to, kind });
  };
  const item = (id: string, kind: CtxKind, label: string, detail: string): CtxItem => {
    let it = items.get(id);
    if (!it) {
      it = { id, kind, label, detail, turns: [], reads: 0, writes: 0, runs: 0, errors: 0 };
      items.set(id, it);
    }
    return it;
  };
  const touch = (it: CtxItem, turn: CtxTurn | undefined) => {
    if (!turn) return;
    if (!it.turns.includes(turn.index)) it.turns.push(turn.index);
    if (!turn.items.includes(it.id)) turn.items.push(it.id);
  };

  let tokens = 0;
  let liveTokens = 0;
  let parts = { user: 0, assistant: 0, tools: 0, summary: 0 };
  let toolCalls = 0;
  let compacted = false;
  let cur: CtxTurn | undefined;
  for (const [mi, m] of msgs.entries()) {
    const size = tokensOf(m.content) + (m.toolCalls ?? []).reduce((n, c) => n + tokensOf(c.argsJson), 0);
    tokens += size;
    if (m.kind === "summary") {
      // what came before now reaches the model only as this summary
      compacted = true;
      liveTokens = size;
      parts = { user: 0, assistant: 0, tools: 0, summary: size };
      continue;
    }
    liveTokens += size;
    if (m.role === "user") parts.user += size;
    else if (m.role === "tool") parts.tools += size;
    else parts.assistant += size;
    if (m.role === "user") {
      const shown = splitContext(m.content ?? "");
      const text = shown.text.trim() || m.content || "";
      cur = { index: turns.length, prompt: text, answer: "", msgIndex: mi, at: m.createdAt, tokens: size, items: [] };
      turns.push(cur);
      const p = item(`p:${cur.index}`, "prompt", `${cur.index + 1}. ${clip(text, 28)}`, text);
      touch(p, cur);
      link(root, p.id, "turn");
      // files attached to the message are in context from the start
      for (const f of shown.files) {
        const it = item(`f:${f}`, "file", base(f), f);
        it.reads++;
        touch(it, cur);
        link(p.id, it.id, "read");
      }
      continue;
    }
    if (cur) {
      cur.tokens += size;
      if (m.role === "assistant" && m.content?.trim()) cur.answer = m.content.trim();
    }
    const from = cur ? `p:${cur.index}` : root;
    for (const c of m.toolCalls ?? []) {
      toolCalls++;
      const a = parse(c.argsJson);
      const res = results.get(c.id);
      const note = (it: CtxItem) => {
        if (res) {
          it.last = clip(res.content, 160);
          if (res.isError) it.errors++;
        }
      };
      if (FILE_READ.has(c.name) || FILE_WRITE.has(c.name)) {
        const path = str(a.path) || ".";
        const it = item(`f:${path}`, "file", base(path), path);
        const write = FILE_WRITE.has(c.name);
        if (write) it.writes++;
        else it.reads++;
        note(it);
        touch(it, cur);
        link(from, it.id, write ? "write" : "read");
        continue;
      }
      if (c.name === "shell") {
        const cmd = str(a.command);
        const it = item(`c:${cmd}`, "command", clip(commandName(cmd), 26), cmd);
        it.runs++;
        note(it);
        touch(it, cur);
        link(from, it.id, "run");
        // a command that names a file the chat already knows uses that file
        for (const hit of cmd.matchAll(PATH_RX)) {
          const f = items.get(`f:${hit[1]}`);
          if (f) link(it.id, f.id, "uses");
        }
        continue;
      }
      if (AGENT.has(c.name)) {
        const who = str(a.session) || str(a.member) || (c.name === "team_delegate" ? "alt ajanlar" : c.name);
        const it = item(`a:${who}`, "agent", clip(who, 24), who);
        it.runs++;
        note(it);
        touch(it, cur);
        link(from, it.id, "talk");
        continue;
      }
      if (/web|fetch|search|browse|http/i.test(c.name)) {
        const target = str(a.url) || str(a.query) || str(a.q) || c.name;
        const it = item(`w:${target}`, "web", clip(target.replace(/^https?:\/\//, ""), 26), target);
        it.runs++;
        note(it);
        touch(it, cur);
        link(from, it.id, "read");
        continue;
      }
      // anything else still counts as something the agent did
      const first = Object.values(a).find((v) => typeof v === "string") as string | undefined;
      const it = item(`o:${c.name}:${first ?? ""}`, "command", clip(`${c.name} ${first ?? ""}`, 26), `${c.name} ${first ?? ""}`.trim());
      it.runs++;
      note(it);
      touch(it, cur);
      link(from, it.id, "run");
    }
  }
  return { turns, items, edges, tokens, liveTokens, parts, messages: msgs.length, toolCalls, compacted };
}

// sharedFiles lists the files two chats both touched: what ties their
// contexts together.
export function sharedFiles(a: SessionContext, b: SessionContext): string[] {
  const out: string[] = [];
  for (const [id, it] of a.items) if (it.kind === "file" && b.items.has(id)) out.push(it.detail);
  return out;
}

// windowOf guesses a model's context window from its name; the maps show
// fill against it, so a rough figure is enough.
export function windowOf(model: string): number {
  const m = model.toLowerCase();
  if (/claude|sonnet|opus|haiku|fable/.test(m)) return 200_000;
  if (/gemini/.test(m)) return 1_000_000;
  if (/grok/.test(m)) return 256_000;
  if (/gpt-4\.1|gpt-5|o3|o4/.test(m)) return 400_000;
  if (/gpt-4o|gpt-4-turbo|deepseek|qwen|llama|mistral/.test(m)) return 128_000;
  return 128_000;
}

// ── the map's assistant ─────────────────────────────────────────────────────

// BRIEF_MARK starts the part of a message to the map's assistant that
// carries the chat's context; the panel shows only what comes before it.
export const BRIEF_MARK = "\n\n[bağlam haritası]";

const kb = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n));
const flat = (s: string, n: number) => {
  const one = s.replace(/\s+/g, " ").trim();
  return one.length > n ? `${one.slice(0, n - 1)}…` : one;
};

function itemLine(it: CtxItem): string {
  const acts = [it.writes && `yazma ${it.writes}`, it.reads && `okuma ${it.reads}`, it.runs && `çalıştırma ${it.runs}`, it.errors && `hata ${it.errors}`].filter(Boolean).join(", ");
  return `${it.detail}${acts ? ` (${acts})` : ""}`;
}

// contextBrief is the chat's context as the map's assistant reads it: sizes,
// every request with what it drew on and how it was answered, the files and
// commands, and what the user has picked on the map. It also teaches the
// assistant the few edits the map can carry out, which the user confirms.
export function contextBrief(ctx: SessionContext, title: string, model: string, window: number, picked?: CtxItem | null): string {
  const out: string[] = [];
  out.push(`Kullanıcı Rove'da "${title}" sohbetinin bağlam haritasına bakıyor ve seninle o sohbetin bağlamı hakkında konuşuyor. Sen o sohbet değilsin; aşağıdaki, onun bağlamının özeti. Kısa ve net cevap ver, kullanıcının dilinde. İsteklerden numarasıyla ("4. istek"), dosya ve komutlardan adıyla bahset: harita, bahsettiklerine doğru kayar.`);
  out.push(`Model: ${model || "?"} · pencere ≈${kb(window)} token · modelin gördüğü ≈${kb(ctx.liveTokens)} token (kullanıcı ${kb(ctx.parts.user)}, yanıtlar ${kb(ctx.parts.assistant)}, araç çıktıları ${kb(ctx.parts.tools)}${ctx.parts.summary ? `, özet ${kb(ctx.parts.summary)}` : ""})${ctx.compacted ? " · eski mesajlar özetlenmiş" : ""} · ${ctx.turns.length} istek, ${ctx.toolCalls} araç çağrısı`);
  const turns = ctx.turns.length > 40 ? ctx.turns.slice(-40) : ctx.turns;
  if (ctx.turns.length > turns.length) out.push(`(ilk ${ctx.turns.length - turns.length} istek kısalık için atlandı)`);
  out.push("İstekler:");
  for (const tn of turns) {
    const used = tn.items.filter((id) => !id.startsWith("p:")).map((id) => ctx.items.get(id)).filter(Boolean) as CtxItem[];
    const u = used.length ? ` → kullandı: ${used.map((it) => `${it.label}${it.writes ? " (yazdı)" : ""}${it.errors ? " (hata)" : ""}`).join(", ")}` : "";
    const a = tn.answer ? ` | yanıt: "${flat(tn.answer, 160)}"` : "";
    out.push(`${tn.index + 1}. "${flat(tn.prompt, 160)}" (≈${kb(tn.tokens)} token)${u}${a}`);
  }
  const files = [...ctx.items.values()].filter((it) => it.kind === "file");
  if (files.length) out.push(`Dosyalar: ${files.map(itemLine).join("; ")}`);
  const cmds = [...ctx.items.values()].filter((it) => it.kind === "command");
  if (cmds.length) out.push(`Komutlar: ${cmds.slice(0, 30).map(itemLine).join("; ")}`);
  if (picked) out.push(`Haritada seçili: ${pickedLine(ctx, picked)}`);
  out.push([
    "Kullanıcı bağlamı düzenlemek isterse, cevabının sonunda şu bloklardan birini öner; kullanıcı onaylarsa uygulanır, sen uygulamış gibi konuşma:",
    "```rove-action",
    '{"action":"compact"}                      // eski mesajları özetleyip bağlamı hafiflet',
    '{"action":"forget","fromTurn":N}          // N. istek ve sonrasını sohbetten sil',
    '{"action":"rename","title":"yeni ad"}     // sohbetin adını değiştir',
    '{"action":"show","turn":N}                // haritada N. isteği göster',
    '{"action":"show","file":"dosya/yolu"}     // haritada bir dosyayı göster',
    "```",
    "Her blokta tek bir eylem olsun. Yalnızca gerçekten işe yarayacaksa öner.",
  ].join("\n"));
  return out.join("\n");
}

export function pickedLine(ctx: SessionContext, it: CtxItem): string {
  if (it.kind === "prompt") {
    const tn = ctx.turns[Number(it.id.slice(2))];
    return tn ? `${tn.index + 1}. istek: "${flat(tn.prompt, 300)}"` : it.label;
  }
  return `${it.kind === "file" ? "dosya" : it.kind === "command" ? "komut" : it.kind} ${itemLine(it)}${it.last ? ` · son çıktı: "${flat(it.last, 160)}"` : ""}`;
}

export type MapAction =
  | { action: "compact" }
  | { action: "forget"; fromTurn: number }
  | { action: "rename"; title: string }
  | { action: "show"; turn?: number; file?: string };

// splitActions takes the rove-action blocks out of an assistant reply: the
// text to show, and the edits to offer as buttons. Anything malformed or
// unknown is dropped rather than shown as raw JSON.
export function splitActions(reply: string): { text: string; actions: MapAction[] } {
  const actions: MapAction[] = [];
  const text = reply.replace(/```rove-action\s*\n([\s\S]*?)```/g, (_, body: string) => {
    for (const line of body.split("\n")) {
      const json = line.replace(/\/\/.*$/, "").trim();
      if (!json) continue;
      try {
        const a = JSON.parse(json) as Record<string, unknown>;
        if (a.action === "compact") actions.push({ action: "compact" });
        else if (a.action === "forget" && Number.isInteger(a.fromTurn) && (a.fromTurn as number) > 0) actions.push({ action: "forget", fromTurn: a.fromTurn as number });
        else if (a.action === "rename" && typeof a.title === "string" && a.title.trim()) actions.push({ action: "rename", title: a.title.trim().slice(0, 80) });
        else if (a.action === "show" && (Number.isInteger(a.turn) || typeof a.file === "string")) actions.push({ action: "show", turn: a.turn as number | undefined, file: a.file as string | undefined });
      } catch { /* not an action */ }
    }
    return "";
  }).trim();
  return { text, actions };
}

// referencedItems finds what a reply talks about on the map: requests by
// number ("4. istek", "istek 4", "#4"), files and commands by name. The map
// glides there, so the user sees what is being discussed.
export function referencedItems(ctx: SessionContext, text: string): string[] {
  const out = new Set<string>();
  const turn = (n: number) => {
    if (n >= 1 && n <= ctx.turns.length) out.add(`p:${n - 1}`);
  };
  for (const m of text.matchAll(/(?:^|[^\d])(\d{1,3})\s*\.\s*(?:istek|isteğ|isteg|request|soru|mesaj)/gi)) turn(Number(m[1]));
  for (const m of text.matchAll(/(?:istek|request)\s*#?(\d{1,3})\b/gi)) turn(Number(m[1]));
  for (const m of text.matchAll(/#(\d{1,3})\b/g)) turn(Number(m[1]));
  for (const it of ctx.items.values()) {
    if (it.kind === "file" && it.label.length >= 4 && it.label !== "." && text.includes(it.label)) out.add(it.id);
    else if ((it.kind === "command" || it.kind === "agent" || it.kind === "web") && it.label.length >= 6 && text.includes(it.label)) out.add(it.id);
    if (out.size >= 16) break;
  }
  return [...out];
}
