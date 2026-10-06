import { describe, expect, it } from "vitest";
import { describeTool, formatCount, toBlocks } from "~/lib/transcript";
import type { Message } from "~/lib/types";

const m = (x: Partial<Message> & Pick<Message, "id" | "role">): Message => ({ sessionId: "s", content: "", createdAt: "", ...x });

describe("transcript", () => {
  it("pairs tool calls with results and keeps order", () => {
    const blocks = toBlocks([
      m({ id: "1", role: "user", content: "fix the guard" }),
      m({ id: "2", role: "assistant", content: "Reading it.", toolCalls: [{ id: "t1", name: "read_file", argsJson: '{"path":"src/guard.ts"}' }] }),
      m({ id: "3", role: "tool", content: "a\nb\nc\n", toolResult: { toolCallId: "t1", name: "read_file", content: "a\nb\nc\n" } }),
      m({ id: "4", role: "assistant", content: "", toolCalls: [{ id: "t2", name: "patch_file", argsJson: '{"path":"src/guard.ts","old_string":"if (!s) {","new_string":"if (!s || s.status !== \\"active\\") {\\n  throw x;"}' }] }),
      m({ id: "5", role: "assistant", content: "Done." }),
    ]);
    expect(blocks.map((b) => b.kind)).toEqual(["user", "text", "tool", "tool", "text"]);
    const read = blocks[2].kind === "tool" ? blocks[2].tool : null;
    expect(read).toMatchObject({ verb: "Read", arg: "src/guard.ts", summary: "3 satır okundu", pending: false });
    const upd = blocks[3].kind === "tool" ? blocks[3].tool : null;
    expect(upd?.verb).toBe("Update");
    expect(upd?.diff?.map((d) => d.sign)).toEqual(["-", "+", "+"]);
    expect(upd?.pending).toBe(true);
    expect(upd?.summary).toBe("2 ekleme, 1 silme");
  });

  it("clips shell output and reports errors", () => {
    const out = Array.from({ length: 9 }, (_, i) => `line ${i}`).join("\n");
    const ok = describeTool("x", "shell", '{"command":"npm test -- subscription"}', { content: out });
    expect(ok).toMatchObject({ verb: "Bash", arg: "npm test -- subscription", more: 4 });
    expect(ok.output).toHaveLength(5);
    const bad = describeTool("y", "shell", '{"command":"rm -rf /"}', { content: "permission denied", isError: true });
    expect(bad).toMatchObject({ isError: true, summary: "permission denied" });
    expect(bad.output).toBeUndefined();
  });

  it("collapses map notices and handles odd input", () => {
    const blocks = toBlocks([
      m({ id: "n", role: "user", content: "[kod haritası] Bu dosyalar bağlam olarak paylaşıldı:\n- `src/a.ts` (ts, 10 lines) — defines: a\n- `src/b.ts` (ts, 3 lines)" }),
    ]);
    expect(blocks[0]).toMatchObject({ kind: "notice", files: ["src/a.ts", "src/b.ts"] });
    expect(describeTool("z", "mcp_github_search", "not json").verb).toBe("github_search");
    expect(formatCount(1834)).toBe("1.8k");
    expect(formatCount(42)).toBe("42");
  });
});

describe("team_delegate", () => {
  it("reads as the goal for one task and as a count for several", () => {
    const one = describeTool("a", "team_delegate", JSON.stringify({ tasks: [{ goal: "find the callers of Parse" }] }));
    expect([one.verb, one.arg]).toEqual(["Delegate", "find the callers of Parse"]);
    const many = describeTool("b", "team_delegate", JSON.stringify({ tasks: [{ goal: "a" }, { goal: "b" }, { goal: "c" }] }));
    expect(many.arg).toBe("3 tasks");
  });

  it("sums up how many finished from the report", () => {
    const report = "TASK 1/3 · subagent · completed · 4 turns\nok\n\nTASK 2/3 · Backend · failed\nfailed: x\n\nTASK 3/3 · subagent · completed\nok";
    const v = describeTool("c", "team_delegate", JSON.stringify({ tasks: [{}, {}, {}] }), { content: report });
    expect(v.summary).toBe("2/3 done");
  });
});
