import { describe, it, expect } from "vitest";
import { matchSlash, filterSlash, lastUserKeep, SLASH } from "../lib/slash";

describe("slash", () => {
  it("matches /new /undo /redo /review", () => {
    expect(matchSlash("/new")?.cmd.id).toBe("new");
    expect(matchSlash("/undo")?.cmd.id).toBe("undo");
    expect(matchSlash("/redo")?.cmd.id).toBe("redo");
    expect(matchSlash("/review merge the PR")?.cmd.id).toBe("review");
    expect(matchSlash("/review merge the PR")?.rest).toBe("merge the PR");
    expect(matchSlash("/reset")?.cmd.id).toBe("new");
    expect(matchSlash("/run")?.cmd.id).toBe("run");
    expect(matchSlash("/sweep")?.cmd.id).toBe("run");
    expect(matchSlash("hello")).toBeNull();
    expect(matchSlash("/nope")).toBeNull();
  });

  it("filters as you type", () => {
    expect(filterSlash("/").map((c) => c.id)).toEqual(SLASH.map((c) => c.id));
    expect(filterSlash("/un").map((c) => c.id)).toEqual(["undo"]);
    expect(filterSlash("/re").map((c) => c.id)).toEqual(["redo", "review"]);
    expect(filterSlash("/rev").map((c) => c.id)).toEqual(["review"]);
    expect(filterSlash("/res").map((c) => c.id)).toEqual(["new"]);
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
