import { describe, it, expect } from "vitest";
import { matchSlash, filterSlash, filterModels, lastUserKeep, pickModel, SLASH } from "../lib/slash";

describe("slash", () => {
  it("matches /new /undo /redo", () => {
    expect(matchSlash("/new")?.cmd.id).toBe("new");
    expect(matchSlash("/undo")?.cmd.id).toBe("undo");
    expect(matchSlash("/redo")?.cmd.id).toBe("redo");
    expect(matchSlash("/rename the PR")?.rest).toBe("the PR");
    expect(matchSlash("/reset")?.cmd.id).toBe("new");
    expect(matchSlash("hello")).toBeNull();
    expect(matchSlash("/nope")).toBeNull();
  });

  it("filters as you type", () => {
    expect(filterSlash("/").map((c) => c.id)).toEqual(SLASH.map((c) => c.id));
    expect(filterSlash("/un").map((c) => c.id)).toEqual(["undo"]);
    expect(filterSlash("/re").map((c) => c.id)).toEqual(["rename", "remember", "restore", "redo"]);
    expect(filterSlash("/rev")).toEqual([]);
    expect(filterSlash("/res").map((c) => c.id)).toEqual(["new", "restore"]);
  });

  it("/models picks a chat's model by name, or opens the picker when unsure", () => {
    expect(matchSlash("/models")?.cmd.id).toBe("models");
    expect(matchSlash("/model gpt")?.rest).toBe("gpt");
    expect(filterSlash("/mo").map((c) => c.id)).toEqual(["models"]);
    expect(matchSlash("/session giriş")?.cmd.id).toBe("session");
    expect(matchSlash("/oturumlar")?.cmd.id).toBe("session");
    expect(filterSlash("/se").map((c) => c.id)).toEqual(["session"]);
    const list = [
      { provider: "openai", model: "gpt-4o" },
      { provider: "openai", model: "gpt-4o-mini" },
      { provider: "anthropic", model: "claude-sonnet-5" },
    ];
    expect(filterModels(list, "GPT").map((m) => m.model)).toEqual(["gpt-4o", "gpt-4o-mini"]);
    expect(filterModels(list, "anthropic/").map((m) => m.model)).toEqual(["claude-sonnet-5"]);
    expect(pickModel(list, "gpt-4o")?.model).toBe("gpt-4o"); // exact wins over "contains"
    expect(pickModel(list, "sonnet")?.model).toBe("claude-sonnet-5"); // the only hit
    expect(pickModel(list, "gpt")).toBeNull(); // two hits: the picker decides
    expect(pickModel(list, "")).toBeNull();
  });

  it("undo keep is index of last user message", () => {
    expect(lastUserKeep([
      { role: "user" },
      { role: "assistant" },
      { role: "user" },
      { role: "assistant" },
    ])).toBe(2);
    expect(lastUserKeep([{ role: "assistant" }])).toBe(1);
    expect(lastUserKeep([])).toBe(0);
  });
});
