// Turns a session's messages into the blocks the terminal view draws:
// "> prompt", "● text", "● Update(path)" with a diff, "● Bash(cmd)" with
// clipped output. Pure so it can be tested without a DOM.

import type { Message } from "./types";

export type DiffLine = { sign: "+" | "-" | " "; text: string; n?: number };

export type ToolView = {
  id: string;
  // the raw call, kept so a live row can be redrawn once its result lands
  name: string;
  args?: string;
  verb: string;
  arg: string;
  summary: string;
  diff?: DiffLine[];
  // how many lines the edit added and removed, before any clipping
  added?: number;
  removed?: number;
  output?: string[];
  more?: number;
  isError?: boolean;
  // a rule would not let this call run; not a fault
  refused?: boolean;
  pending?: boolean;
};

// ToolOutcome is what came back from one tool call.
export type ToolOutcome = { content: string; isError?: boolean; kind?: string };

export type Block =
  | { kind: "user"; id: string; text: string }
  | { kind: "notice"; id: string; title: string; files: string[] }
  | { kind: "text"; id: string; text: string }
  | { kind: "tool"; id: string; tool: ToolView };

const NOTICE = /^\[(bağlam haritası|kod haritası)[^\]]*\]/;
// A written file is shown in a card that scrolls, so it can hold a real
// edit rather than a taste of one.
const MAX_DIFF = 400;
const MAX_OUT = 5;

function parseArgs(json: string | undefined): Record<string, unknown> {
  if (!json) return {};
  try {
    const v = JSON.parse(json);
    return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function str(v: unknown): string {
  return typeof v === "string" ? v : v == null ? "" : JSON.stringify(v);
}

function lines(s: string): string[] {
  const out = s.replace(/\r\n/g, "\n").split("\n");
  if (out.length && out[out.length - 1] === "") out.pop();
  return out;
}

function clipArg(s: string, n = 72): string {
  const one = s.replace(/\s+/g, " ").trim();
  return one.length > n ? `${one.slice(0, n - 1)}…` : one;
}

// describeTool renders one tool call; result is the tool message's content
// when the call has finished.
export function describeTool(id: string, name: string, argsJson: string | undefined, result?: ToolOutcome): ToolView {
  const a = parseArgs(argsJson);
  const path = str(a.path);
  const v: ToolView = { id, name, args: argsJson, verb: name, arg: "", summary: "", isError: result?.isError, refused: result?.kind === "refused", pending: !result };
  const outLines = result ? lines(result.content) : [];
  const clipOut = () => {
    v.output = outLines.slice(0, MAX_OUT);
    if (outLines.length > MAX_OUT) v.more = outLines.length - MAX_OUT;
  };
  switch (name) {
    case "read_file":
      v.verb = "Read";
      v.arg = path;
      if (result && !result.isError) v.summary = `${outLines.length} satır okundu`;
      break;
    case "write_file": {
      v.verb = "Write";
      v.arg = path;
      const body = lines(str(a.content));
      v.diff = body.slice(0, MAX_DIFF).map((text, i) => ({ sign: "+", text, n: i + 1 }));
      if (body.length > MAX_DIFF) v.more = body.length - MAX_DIFF;
      v.added = body.length;
      v.removed = 0;
      v.summary = `${body.length} satır yazıldı`;
      break;
    }
    case "patch_file": {
      v.verb = "Update";
      v.arg = path;
      const del = lines(str(a.old_string));
      const add = lines(str(a.new_string));
      const all: DiffLine[] = [...del.map((text) => ({ sign: "-" as const, text })), ...add.map((text) => ({ sign: "+" as const, text }))];
      v.diff = all.slice(0, MAX_DIFF);
      if (all.length > MAX_DIFF) v.more = all.length - MAX_DIFF;
      v.added = add.length;
      v.removed = del.length;
      v.summary = `${add.length} ekleme, ${del.length} silme`;
      break;
    }
    case "list_dir":
      v.verb = "List";
      v.arg = path || ".";
      if (result && !result.isError) v.summary = `${outLines.length} öğe`;
      break;
    case "shell":
      v.verb = "Bash";
      v.arg = clipArg(str(a.command), 90);
      if (result) clipOut();
      break;
    case "git_status":
      v.verb = "Git";
      v.arg = "status";
      if (result) clipOut();
      break;
    case "git_commit":
      v.verb = "Commit";
      v.arg = clipArg(str(a.message));
      break;
    case "graph_query":
      v.verb = "Map";
      v.arg = str(a.q);
      break;
    case "graph_neighbors":
      v.verb = "Map";
      v.arg = str(a.target);
      break;
    case "graph_path":
      v.verb = "Path";
      v.arg = `${str(a.from)} → ${str(a.to)}`;
      break;
    case "graph_impact":
      v.verb = "Impact";
      v.arg = Array.isArray(a.files) ? (a.files as unknown[]).map(str).join(", ") : "";
      break;
    case "graph_changed":
      v.verb = "Changed";
      v.arg = str(a.base) || "HEAD";
      break;
    case "linked_sessions":
      v.verb = "Sessions";
      break;
    case "team_delegate": {
      // one task reads as its goal; several as a count, the roster has them
      v.verb = "Delegate";
      const list = (Array.isArray(a.tasks) ? a.tasks : Array.isArray(a.assignments) ? a.assignments : []) as Record<string, unknown>[];
      v.arg = list.length === 1 ? clipArg(str(list[0].goal) || str(list[0].task)) : `${list.length} tasks`;
      if (result) {
        const all = (result.content.match(/^TASK \d+\/\d+ /gm) ?? []).length;
        const ok = (result.content.match(/^TASK \d+\/\d+ · .* · completed/gm) ?? []).length;
        v.summary = all ? `${ok}/${all} done` : "";
      }
      break;
    }
    case "message_session":
      v.verb = "Message";
      v.arg = str(a.session);
      break;
    // automations set up from a chat: what was set up reads as one line,
    // the tool's answer (what it made, how to switch it off) as its summary
    case "automation_list":
      v.verb = "Automations";
      break;
    case "automation_watch":
      v.verb = "Watch";
      v.arg = clipArg(`${str(a.agent)} ← ${str(a.chat) || "this chat"}`);
      if (result && !result.isError) v.summary = clipArg(result.content, 160);
      break;
    case "automation_link":
      v.verb = "Cable";
      v.arg = clipArg(str(a.chat));
      if (result && !result.isError) v.summary = clipArg(result.content, 160);
      break;
    case "automation_schedule":
      v.verb = "Schedule";
      v.arg = clipArg(`${str(a.agent)} · ${a.every_minutes ? `${a.every_minutes} min` : str(a.daily_at)}`);
      if (result && !result.isError) v.summary = clipArg(result.content, 160);
      break;
    case "automation_monitor":
      v.verb = "Monitor";
      v.arg = clipArg(`${str(a.agent)} · ${str(a.name) || str(a.command)}`);
      if (result && !result.isError) v.summary = clipArg(result.content, 160);
      break;
    case "automation_remove":
      v.verb = "Remove";
      v.arg = str(a.id);
      if (result && !result.isError) v.summary = result.content;
      break;
    default: {
      v.verb = name.startsWith("mcp_") ? name.slice(4) : name;
      const first = Object.values(a).find((x) => typeof x === "string") as string | undefined;
      v.arg = first ? clipArg(first) : "";
      if (result) clipOut();
    }
  }
  if (result?.isError) {
    // a refusal's text is written for the model ("do this instead…"); the
    // row shows only its first sentence
    const text = v.refused ? (result.content.split(/(?<=\.)\s/)[0] ?? result.content) : result.content;
    v.summary = clipArg(text || "hata", 140);
    v.output = undefined;
    v.more = undefined;
  }
  return v;
}

type RawMessage = Message & {
  toolCalls?: { id: string; name: string; argsJson?: string }[] | null;
  toolResult?: (ToolOutcome & { toolCallId: string; name: string }) | null;
};

// toBlocks flattens messages into drawable blocks, pairing each tool call
// with its result message.
export function toBlocks(messages: RawMessage[]): Block[] {
  const results = new Map<string, ToolOutcome>();
  for (const m of messages) {
    if (m.role === "tool" && m.toolResult?.toolCallId) {
      results.set(m.toolResult.toolCallId, { content: m.toolResult.content ?? m.content, isError: m.toolResult.isError, kind: m.toolResult.kind });
    }
  }
  const out: Block[] = [];
  for (const m of messages) {
    if (m.role === "tool" || m.role === "system") continue;
    if (m.role === "user") {
      const hit = m.content.match(NOTICE);
      if (hit) {
        const files = m.content.split("\n").filter((l) => l.startsWith("- ")).map((l) => l.slice(2).replace(/`/g, "").split(/\s[—(]/)[0].trim());
        out.push({ kind: "notice", id: m.id, title: m.content.split("\n")[0], files: files.slice(0, 8) });
      } else {
        out.push({ kind: "user", id: m.id, text: m.content });
      }
      continue;
    }
    if (m.content.trim()) out.push({ kind: "text", id: m.id, text: m.content });
    for (const tc of m.toolCalls ?? []) {
      out.push({ kind: "tool", id: `${m.id}:${tc.id}`, tool: describeTool(tc.id, tc.name, tc.argsJson, results.get(tc.id)) });
    }
  }
  return out;
}

// estimateTokens is the ~4 characters per token rule used for the live
// "↓ N tokens" counter.
export function estimateTokens(chars: number): number {
  return Math.round(chars / 4);
}

export function formatCount(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
}
