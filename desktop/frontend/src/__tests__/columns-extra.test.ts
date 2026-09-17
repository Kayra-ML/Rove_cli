import { describe, it, expect } from "vitest";
import { columnLabel, isPrimaryWorkspace } from "../lib/columns";
import { COLUMNS } from "../lib/types";

describe("columns", () => {
  it("labels every column", () => {
    for (const c of COLUMNS) {
      expect(columnLabel(c).length).toBeGreaterThan(0);
    }
  });
  it("primary views", () => {
    expect(isPrimaryWorkspace("chat")).toBe(true);
    expect(isPrimaryWorkspace("kanban")).toBe(true);
    expect(isPrimaryWorkspace("terminal")).toBe(false);
  });
});
