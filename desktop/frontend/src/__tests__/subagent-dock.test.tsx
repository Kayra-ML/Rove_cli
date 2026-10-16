import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Session, SubagentTask } from "../lib/types";

// The chat's subagents over the message box: who is on what, what each is
// doing, where its work ended up — and the handles to stop, steer, apply or
// discard one.
const calls: [string, unknown][] = [];
let listed: SubagentTask[] = [];
const subs = new Map<string, (ev: { payload?: unknown }) => void>();
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    if (method === "subagent.list") return listed;
    return { ok: true };
  }),
  onReconnect: vi.fn(() => () => {}),
  subscribeEvents: vi.fn((filter: string, cb: (ev: { payload?: unknown }) => void) => {
    subs.set(filter, cb);
    return () => subs.delete(filter);
  }),
}));
vi.mock("../lib/toast", () => ({ toast: vi.fn() }));

import { SubagentDock } from "../app/SubagentDock";

const chat: Session = { id: "C", title: "chat", agentId: "A", workspaceId: "W", updatedAt: "" };
const task = (over: Partial<SubagentTask>): SubagentTask => ({
  id: "t1", batch: "b1", index: 1, of: 2, parentId: "C", sessionId: "S1", goal: "build the login form",
  status: "running", startedAt: new Date(Date.now() - 5000).toISOString(), ...over,
});
const fire = (type: string, payload: unknown) => act(() => { subs.get(type)?.({ payload }); });

describe("SubagentDock", () => {
  beforeEach(() => { calls.length = 0; listed = []; subs.clear(); });
  afterEach(cleanup);

  it("is not there until something was handed out", async () => {
    const { container } = render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    await waitFor(() => expect(calls.some(([m]) => m === "subagent.list")).toBe(true));
    expect(container.querySelector(".subagents")).toBeNull();
  });

  // a card is picked by its name; its details and buttons open below the strip
  const pick = async (name: string) => {
    fireEvent.click((await screen.findByText(name)).closest(".sa-card")!);
  };

  it("shows how many there are, and a card each with its state and what it is doing now", async () => {
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    await waitFor(() => expect(subs.has("subagent.updated")).toBe(true));
    fire("subagent.updated", { sessionId: "C", tasks: [
      task({}),
      task({ id: "t2", index: 2, sessionId: "S2", goal: "add POST /login", status: "completed", summary: "route added\nmore" }),
    ] });
    expect(await screen.findByText("Agentlar")).toBeTruthy();
    expect(document.querySelector(".sa-total")?.textContent).toBe("2");
    expect(screen.getByText("1 çalışıyor · 1 bitti")).toBeTruthy();
    // plain subagents, no characters: each is named by its place in the call
    expect(screen.getByText("Alt ajan 1")).toBeTruthy();
    expect(screen.getByText("Alt ajan 2")).toBeTruthy();
    // a finished one shows the first line of its report
    expect(screen.getByText("route added")).toBeTruthy();
    // a running one shows its latest tool call
    fire("tool.start", { sessionId: "S1", name: "read_file", args: { path: "src/login.tsx" } });
    await waitFor(() => expect(document.querySelector(".sa-card.st-running .sa-card-line")?.textContent).toContain("login.tsx"));
  });

  it("only listens to its own chat", async () => {
    const { container } = render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    await waitFor(() => expect(subs.has("subagent.updated")).toBe(true));
    fire("subagent.updated", { sessionId: "OTHER", tasks: [task({})] });
    expect(container.querySelector(".subagents")).toBeNull();
  });

  it("stops and steers a running one", async () => {
    listed = [task({})];
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    await pick("Alt ajan 1");
    fireEvent.click(screen.getByRole("button", { name: "Durdur" }));
    await waitFor(() => expect(calls.find(([m]) => m === "subagent.stop")?.[1]).toEqual({ id: "t1" }));

    fireEvent.click(screen.getByRole("button", { name: "Yönlendir" }));
    const box = screen.getByPlaceholderText("Ne yapsın? Bir sonraki adımında okur.");
    fireEvent.change(box, { target: { value: "use the v2 API" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(calls.find(([m]) => m === "subagent.steer")?.[1]).toEqual({ id: "t1", text: "use the v2 API" }));
  });

  // After Orca: work that could not land waits in its own copy, and the user
  // decides — take it, or throw it away.
  it("offers to apply or discard work that is waiting", async () => {
    listed = [task({ status: "completed", worktree: {
      repo: "/r", path: "/wt/t1", branch: "rove/sub-t1", base: "abc", added: 3, removed: 1,
      files: ["shared.txt"], applied: false, pending: "clashes with other changes to the same lines: git apply --check", reason: "clash",
    } })];
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    // its card is marked as waiting on the user
    expect((await screen.findByText("Alt ajan 1")).closest(".sa-card")?.className).toContain("wait");
    await pick("Alt ajan 1");
    // the reason in the user's words; git's own message only in the tooltip
    const why = screen.getByText("bekliyor — başka bir değişiklikle aynı satırlara dokunuyor");
    expect(why.getAttribute("title")).toContain("git apply --check");
    expect(document.querySelector(".sa-diff")?.textContent).toContain("+3 −1 · 1 dosya");
    fireEvent.click(screen.getByRole("button", { name: "Uygula" }));
    await waitFor(() => expect(calls.find(([m]) => m === "subagent.apply")?.[1]).toEqual({ id: "t1" }));
    fireEvent.click(screen.getByRole("button", { name: "At" }));
    await waitFor(() => expect(calls.find(([m]) => m === "subagent.discard")?.[1]).toEqual({ id: "t1" }));
  });

  it("says so when the work was thrown away, and offers nothing more", async () => {
    listed = [task({ status: "completed", worktree: {
      repo: "/r", path: "", branch: "b", base: "abc", added: 1, removed: 1, files: ["x"], applied: false,
      pending: "discarded", reason: "discarded",
    } })];
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    await pick("Alt ajan 1");
    expect(screen.getByText("atıldı")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Uygula" })).toBeNull();
  });

  it("says when the work landed", async () => {
    listed = [task({ status: "completed", worktree: {
      repo: "/r", path: "", branch: "", base: "abc", added: 2, removed: 0, files: ["a.go"], applied: true,
    } })];
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    await pick("Alt ajan 1");
    expect(screen.getByText("projeye uygulandı")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Uygula" })).toBeNull();
  });

  it("opens a one-off subagent's channel as a worker's", async () => {
    listed = [task({ status: "completed" })];
    const onOpen = vi.fn();
    render(<SubagentDock session={chat} lang="tr" onOpen={onOpen} />);
    await pick("Alt ajan 1");
    fireEvent.click(screen.getByRole("button", { name: "Dökümü aç" }));
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: "S1", parentId: "C", space: "worker" }));
  });

  // Every subagent of the chat is on the strip: those at work first, then
  // those waiting on the user, then the finished ones; the count is them all.
  it("keeps every subagent on the strip, at work first", async () => {
    listed = [
      task({ id: "old", batch: "b0", goal: "old and done", status: "completed", endedAt: "2026-10-16T10:00:00Z" }),
      task({ id: "wait", batch: "b0", index: 2, goal: "old but waiting", status: "completed", worktree: {
        repo: "/r", path: "/wt/w", branch: "b", base: "x", added: 1, removed: 0, files: ["f"], applied: false, pending: "clash",
      } }),
      task({ id: "new", batch: "b1", goal: "the latest", status: "running" }),
    ];
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    expect(await screen.findByText("the latest")).toBeTruthy();
    const goals = [...document.querySelectorAll(".sa-card .sa-card-goal")].map((e) => e.textContent);
    expect(goals).toEqual(["the latest", "old but waiting", "old and done"]);
    expect(document.querySelector(".sa-total")?.textContent).toBe("3");
  });

  it("pages through more than five", async () => {
    listed = Array.from({ length: 7 }, (_, i) => task({ id: `t${i}`, index: i + 1, sessionId: `S${i}`, goal: `part ${i + 1}` }));
    render(<SubagentDock session={chat} lang="tr" onOpen={() => {}} />);
    expect(await screen.findByText("part 7")).toBeTruthy();
    expect(document.querySelector(".sa-total")?.textContent).toBe("7");
    expect(screen.getByRole("button", { name: "Sonraki agentlar" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Önceki agentlar" })).toBeTruthy();
  });
});
