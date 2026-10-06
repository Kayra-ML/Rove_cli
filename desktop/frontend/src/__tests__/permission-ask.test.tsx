import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

// The question an agent's run waits on: it comes over the middle of the
// window, and the answer goes back to the daemon.
const calls: [string, unknown][] = [];
let pending: unknown[] = [];
const subs = new Map<string, (ev: { payload?: unknown }) => void>();
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    if (method === "permission.asks") return pending;
    return { ok: true };
  }),
  subscribeEvents: vi.fn((filter: string, cb: (ev: { payload?: unknown }) => void) => {
    subs.set(filter, cb);
    return () => subs.delete(filter);
  }),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr" }) }));

import { PermissionAsk } from "../app/PermissionAsk";

const ask = (id: string, detail = "echo merhaba") => ({ id, sessionId: "S", action: "shell", tool: "shell", detail });
const fire = (type: string, payload: unknown) => act(() => { subs.get(type)?.({ payload }); });

describe("PermissionAsk", () => {
  beforeEach(() => { calls.length = 0; pending = []; subs.clear(); });
  afterEach(cleanup);

  it("stays out of the way until something asks", async () => {
    render(<PermissionAsk />);
    await waitFor(() => expect(calls.some(([m]) => m === "permission.asks")).toBe(true));
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("shows what is being asked and sends the answer", async () => {
    render(<PermissionAsk />);
    await waitFor(() => expect(subs.has("permission.ask")).toBe(true));
    fire("permission.ask", ask("q1"));
    const dialog = await screen.findByRole("alertdialog", { name: "İzin gerekiyor" });
    expect(dialog.textContent).toContain("komut çalıştırma");
    expect(dialog.textContent).toContain("echo merhaba");
    fireEvent.click(screen.getByRole("button", { name: "Bu seferlik izin ver" }));
    await waitFor(() => expect(calls.find(([m]) => m === "permission.answer")?.[1]).toEqual({ id: "q1", allow: true, remember: false }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("keeps the answer when asked to, and refuses on Escape", async () => {
    render(<PermissionAsk />);
    await waitFor(() => expect(subs.has("permission.ask")).toBe(true));
    fire("permission.ask", ask("q1"));
    await screen.findByRole("alertdialog");
    fireEvent.click(screen.getByRole("button", { name: "Hep izin ver" }));
    await waitFor(() => expect(calls.find(([m]) => m === "permission.answer")?.[1]).toEqual({ id: "q1", allow: true, remember: true }));

    calls.length = 0;
    fire("permission.ask", ask("q2"));
    await screen.findByRole("alertdialog");
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(calls.find(([m]) => m === "permission.answer")?.[1]).toEqual({ id: "q2", allow: false, remember: false }));
  });

  it("queues several questions and drops one answered elsewhere", async () => {
    render(<PermissionAsk />);
    await waitFor(() => expect(subs.has("permission.ask")).toBe(true));
    fire("permission.ask", ask("q1", "ilk"));
    fire("permission.ask", ask("q2", "ikinci"));
    fire("permission.ask", ask("q1", "ilk")); // the same question twice changes nothing
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("ilk");
    expect(dialog.textContent).toContain("1 istek daha");
    // the daemon says q1 was settled (another window, or it stopped waiting)
    fire("permission.answered", { id: "q1", allow: false });
    await waitFor(() => expect(screen.getByRole("alertdialog").textContent).toContain("ikinci"));
    expect(screen.getByRole("alertdialog").textContent).not.toContain("istek daha");
  });

  it("catches up on questions asked while the app was away", async () => {
    pending = [ask("old", "eski komut")];
    render(<PermissionAsk />);
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("eski komut");
  });
});
