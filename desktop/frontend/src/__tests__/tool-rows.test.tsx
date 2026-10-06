import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ToolRows } from "../app/ToolRows";

// A turn's tool calls read as a quiet log: one line each, and a turn with
// many calls folds into "first + N" until it is opened.
describe("ToolRows", () => {
  afterEach(cleanup);
  const call = (i: number) => ({ id: `c${i}`, name: "shell", argsJson: JSON.stringify({ command: `echo ${i}` }) });
  const lines = () => Array.from(document.querySelectorAll(".tool-chip")).map((r) => r.textContent);

  it("shows a few calls as they are, with errors marked", () => {
    const results = new Map([["c1", { content: "fatal: not a git repository", isError: true }]]);
    render(<ToolRows calls={[call(0), call(1)]} results={results} lang="tr" />);
    expect(document.querySelectorAll(".tool-chip")).toHaveLength(2);
    expect(document.querySelectorAll(".tool-chip.err")).toHaveLength(1);
    expect(document.querySelector(".tool-chip.pending")).toBeTruthy(); // c0 has no result yet
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("a refused call is not shown as an error, and reads as one line", () => {
    const refusal = {
      content: "shell is not allowed in this chat (rule: ask). Do not try it again: do the work another way, or ask the user to allow shell in Settings → Permissions.",
      isError: true,
      kind: "refused",
    };
    render(<ToolRows calls={[call(0)]} results={new Map([["c0", refusal]])} lang="tr" />);
    const row = document.querySelector(".tool-chip")!;
    expect(row.className).toContain("refused");
    expect(row.className).not.toContain("err");
    // only the first sentence; the whole text is on hover
    expect(row.textContent).toContain("shell is not allowed in this chat (rule: ask).");
    expect(row.textContent).not.toContain("Settings");
    expect(row.getAttribute("title")).toContain("Settings");
  });

  it("a call that really failed is still marked as an error", () => {
    const failed = { content: "mkdir /Users/x: permission denied", isError: true };
    render(<ToolRows calls={[call(0)]} results={new Map([["c0", failed]])} lang="tr" />);
    const row = document.querySelector(".tool-chip")!;
    expect(row.className).toContain("err");
    expect(row.className).not.toContain("refused");
  });

  it("a written file is a shut card naming the file and its counts", () => {
    const write = { id: "w", name: "write_file", argsJson: JSON.stringify({ path: "src/app/matris.py", content: "bir\niki\nüç" }) };
    render(<ToolRows calls={[write]} results={new Map([["w", { content: "ok" }]])} lang="tr" />);
    const card = document.querySelector(".edit-card")!;
    // shut until asked: the header says enough, and a long diff would bury the turn
    expect(card.className).not.toContain("open");
    expect(document.querySelector(".edit-body")).toBeNull();
    expect(card.querySelector(".edit-path")?.textContent).toBe("matris.py");
    expect(card.querySelector(".edit-path")?.getAttribute("title")).toBe("src/app/matris.py");
    expect(card.querySelector(".edit-add")?.textContent).toBe("+3");
    expect(card.querySelector(".edit-del")).toBeNull();
  });

  it("the caret opens the diff and closes it again", () => {
    const write = { id: "w", name: "write_file", argsJson: JSON.stringify({ path: "src/app/matris.py", content: "bir\niki\nüç" }) };
    render(<ToolRows calls={[write]} results={new Map([["w", { content: "ok" }]])} lang="tr" />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    const card = document.querySelector(".edit-card")!;
    expect(card.className).toContain("open");
    expect(Array.from(card.querySelectorAll(".edit-line")).map((l) => l.textContent)).toEqual(["+bir", "+iki", "+üç"]);
    expect(card.querySelectorAll(".edit-line.add")).toHaveLength(3);
    fireEvent.click(screen.getByRole("button", { expanded: true }));
    expect(document.querySelector(".edit-body")).toBeNull();
  });

  it("a patch shows what went and what came, and counts both", () => {
    const patch = { id: "p", name: "patch_file", argsJson: JSON.stringify({ path: "main.go", old_string: "eski", new_string: "yeni\nsatır" }) };
    render(<ToolRows calls={[patch]} results={new Map([["p", { content: "ok" }]])} lang="tr" />);
    expect(document.querySelector(".edit-add")?.textContent).toBe("+2");
    expect(document.querySelector(".edit-del")?.textContent).toBe("−1");
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    expect(Array.from(document.querySelectorAll(".edit-line")).map((l) => l.className.replace("edit-line ", ""))).toEqual(["del", "add", "add"]);
  });

  it("edits stay in view while the plain calls around them fold", () => {
    const write = { id: "w", name: "write_file", argsJson: JSON.stringify({ path: "a.py", content: "x" }) };
    const calls = [call(0), call(1), write, call(2), call(3), call(4)];
    render(<ToolRows calls={calls} results={new Map()} lang="tr" />);
    expect(document.querySelector(".edit-card")).toBeTruthy();
    expect(lines()).toHaveLength(1); // the five shell calls folded to one
    fireEvent.click(screen.getByRole("button", { name: "+ 4 komut" }));
    expect(lines()).toHaveLength(5);
    expect(document.querySelector(".edit-card")).toBeTruthy();
  });

  it("folds many calls into one line and opens them on a click", () => {
    const calls = [0, 1, 2, 3, 4].map(call);
    render(<ToolRows calls={calls} results={new Map()} lang="tr" />);
    expect(lines()).toHaveLength(1);
    expect(lines()[0]).toContain("echo 0");
    fireEvent.click(screen.getByRole("button", { name: "+ 4 komut" }));
    expect(lines()).toHaveLength(5);
    fireEvent.click(screen.getByRole("button", { name: "Daralt" }));
    expect(lines()).toHaveLength(1);
  });
});
