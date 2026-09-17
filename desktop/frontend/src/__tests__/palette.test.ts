import { describe, it, expect } from "vitest";
import { filterSlash } from "../lib/slash";

describe("command palette slash filter", () => {
  it("surfaces slash cmds from /un", () => {
    expect(filterSlash("/un").map((c) => c.id)).toEqual(["undo"]);
  });
});
