import { describe, expect, it } from "vitest";
import { COLUMNS } from "../lib/types";
import { columnLabel, isPrimaryWorkspace } from "../lib/columns";

describe("workspace modes", () => {
  it("chat and kanban are primary and switchable", () => {
    expect(isPrimaryWorkspace("chat")).toBe(true);
    expect(isPrimaryWorkspace("kanban")).toBe(true);
    expect(COLUMNS).toEqual(["backlog", "ready", "running", "review", "done", "blocked"]);
    expect(columnLabel("review")).toBe("Review");
  });
});
