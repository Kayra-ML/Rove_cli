import { describe, expect, it } from "vitest";
import { filterFiles, mentionAt, mentionedPaths, splitContext, withFiles } from "../lib/attachments";

describe("@file mentions", () => {
  it("finds the mention being typed", () => {
    expect(mentionAt("bak @src/ma", 11)).toEqual({ start: 4, query: "src/ma" });
    expect(mentionAt("@", 1)).toEqual({ start: 0, query: "" });
    expect(mentionAt("mail@host", 9)).toBeNull(); // not after a space
    expect(mentionAt("bak @src done", 13)).toBeNull(); // caret past the word
    expect(mentionAt("ara @codebase", 13)).toBeNull(); // its own feature
  });

  it("ranks file names before paths and keeps only real files", () => {
    const files = ["src/app/Chat.tsx", "src/chat/index.ts", "docs/chat.md", "go.mod"];
    expect(filterFiles(files, "chat")).toEqual(["src/app/Chat.tsx", "docs/chat.md", "src/chat/index.ts"]);
    const set = new Set(files);
    expect(mentionedPaths("şuna bak @go.mod, ve @src/app/Chat.tsx ile @yok.ts @go.mod", set)).toEqual(["go.mod", "src/app/Chat.tsx"]);
  });

  it("sends file content before the text, and shows it back as chips", () => {
    const sent = withFiles("düzelt", [{ path: "a.go", content: "package a" }, { path: "b.go", content: "x".repeat(20000) }]);
    expect(sent.startsWith('<file path="a.go">\npackage a\n</file>')).toBe(true);
    expect(sent).toContain("…(cut)");
    expect(sent.endsWith("düzelt")).toBe(true);
    expect(splitContext(sent)).toEqual({ text: "düzelt", files: ["a.go", "b.go"], codebase: false });
    expect(splitContext("<codebase-context>\n### x\n</codebase-context>\n\nsoru").codebase).toBe(true);
    expect(withFiles("aynı", [])).toBe("aynı");
  });
});
