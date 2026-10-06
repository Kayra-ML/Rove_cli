import { describe, expect, it } from "vitest";
import type { Message } from "../lib/types";
import { BRIEF_MARK, buildSessionContext, commandName, contextBrief, referencedItems, sharedFiles, splitActions, windowOf } from "../lib/sessionContext";

// A chat's context, read from its history: the requests, and what each one
// drew on — files read and written, commands run.
const msg = (over: Partial<Message> & Record<string, unknown>): Message =>
  ({ id: String(Math.random()), sessionId: "S", role: "user", content: "", createdAt: "2026-10-01T10:00:00Z", ...over }) as Message;

const history: Message[] = [
  msg({ role: "user", content: "numpy örneği yaz" }),
  msg({ role: "assistant", content: "yazıyorum", toolCalls: [{ id: "c1", name: "write_file", argsJson: JSON.stringify({ path: "/tmp/a.py", content: "print(1)\n" }) }] } as never),
  msg({ role: "tool", content: "wrote /tmp/a.py", toolResult: { toolCallId: "c1", name: "write_file", content: "wrote /tmp/a.py", isError: false } } as never),
  msg({ role: "assistant", content: "", toolCalls: [{ id: "c2", name: "shell", argsJson: JSON.stringify({ command: "cd /tmp && python3 /tmp/a.py" }) }] } as never),
  msg({ role: "tool", content: "1", toolResult: { toolCallId: "c2", name: "shell", content: "1", isError: false } } as never),
  msg({ role: "user", content: "şimdi oku" }),
  msg({ role: "assistant", content: "", toolCalls: [{ id: "c3", name: "read_file", argsJson: JSON.stringify({ path: "/tmp/a.py" }) }] } as never),
  msg({ role: "tool", content: "boom", toolResult: { toolCallId: "c3", name: "read_file", content: "boom", isError: true } } as never),
];

describe("buildSessionContext", () => {
  it("turns a history into requests and what they touched", () => {
    const c = buildSessionContext(history, "S", "Numpy");
    expect(c.turns.map((t) => t.prompt)).toEqual(["numpy örneği yaz", "şimdi oku"]);
    expect(c.toolCalls).toBe(3);
    const file = c.items.get("f:/tmp/a.py")!;
    expect(file).toMatchObject({ kind: "file", label: "a.py", writes: 1, reads: 1, errors: 1, turns: [0, 1] });
    const cmd = [...c.items.values()].find((i) => i.kind === "command")!;
    expect(cmd.label).toBe("python3 /tmp/a.py");
    // the chat → each request → what it used; running the file links to it
    const edge = (from: string, to: string) => c.edges.find((e) => e.from === from && e.to === to)?.kind;
    expect(edge("s:S", "p:0")).toBe("turn");
    expect(edge("p:0", "f:/tmp/a.py")).toBe("write");
    expect(edge("p:1", "f:/tmp/a.py")).toBe("read");
    expect(edge(cmd.id, "f:/tmp/a.py")).toBe("uses");
    expect(c.liveTokens).toBe(c.tokens);
  });

  it("counts only the summary and what follows once compacted", () => {
    const c = buildSessionContext([...history, msg({ role: "assistant", kind: "summary", content: "x".repeat(40) }), msg({ role: "user", content: "devam" })], "S", "");
    expect(c.compacted).toBe(true);
    expect(c.liveTokens).toBeLessThan(c.tokens);
    expect(c.liveTokens).toBe(10 + 2);
  });

  it("finds the files two chats share", () => {
    const a = buildSessionContext(history, "A", "");
    const b = buildSessionContext([history[0], history[6], history[7]], "B", "");
    expect(sharedFiles(a, b)).toEqual(["/tmp/a.py"]);
  });

  it("names commands and model windows", () => {
    expect(commandName("cd /tmp && cd x; ls -la foo bar")).toBe("ls -la foo");
    expect(windowOf("claude-sonnet-4-6")).toBe(200_000);
    expect(windowOf("gpt-4o")).toBe(128_000);
  });
});

// The map's assistant reads the chat as a brief, and offers edits as blocks
// the panel turns into buttons.
describe("the map's assistant", () => {
  it("briefs the requests, what they used, and the edits it may offer", () => {
    const c = buildSessionContext(history, "S", "Numpy");
    expect(c.turns.map((x) => x.msgIndex)).toEqual([0, 5]);
    const b = contextBrief(c, "Numpy", "gpt-4o", 128_000, c.items.get("f:/tmp/a.py"));
    expect(b).toContain('"Numpy" sohbetinin');
    expect(b).toContain('1. "numpy örneği yaz"');
    expect(b).toContain("a.py (yazdı)");
    expect(b).toContain("Haritada seçili: dosya /tmp/a.py");
    expect(b).toContain("```rove-action");
    expect(BRIEF_MARK.startsWith("\n\n")).toBe(true);
  });

  it("turns action blocks into edits and keeps them out of the text", () => {
    const reply = "Bağlamın çoğu araç çıktısı.\n\n```rove-action\n{\"action\":\"forget\",\"fromTurn\":2}\n```\n\n```rove-action\n{\"action\":\"compact\"} // hafiflet\n```\n```rove-action\n{\"action\":\"rm -rf\"}\nnot json\n```";
    const { text, actions } = splitActions(reply);
    expect(text).toBe("Bağlamın çoğu araç çıktısı.");
    expect(actions).toEqual([{ action: "forget", fromTurn: 2 }, { action: "compact" }]);
    expect(splitActions('```rove-action\n{"action":"forget","fromTurn":0}\n```').actions).toEqual([]);
    expect(splitActions('```rove-action\n{"action":"show","file":"a.py"}\n```').actions).toEqual([{ action: "show", file: "a.py", turn: undefined }]);
  });
});

describe("referencedItems", () => {
  it("finds the requests, files and commands a reply talks about", () => {
    const c = buildSessionContext(history, "S", "");
    expect(referencedItems(c, "Bunu 2. istekte okudu, a.py dosyasını da 1. isteğe yazdı.").sort()).toEqual(["f:/tmp/a.py", "p:0", "p:1"]);
    expect(referencedItems(c, "request 2 and #1")).toEqual(["p:1", "p:0"]);
    // numbers past the chat and plain prose point nowhere
    expect(referencedItems(c, "9. istek yok; genel bir cevap.")).toEqual([]);
  });
});
