import { afterEach, describe, it, expect, vi, beforeEach } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Session } from "../lib/types";

// A reply still running in one chat must not show as "thinking" (with a stop
// button) in another chat, or in a new draft. Regression for run state that
// was keyed by agent — which every session shares.

type Handler = (ev: { type: string; topic?: string; payload?: unknown }) => void;
const handlers = new Map<string, Set<Handler>>();
let releaseSend: (() => void) | null = null;

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string) => {
    switch (method) {
      case "agent.list":
        return [{ id: "a1", name: "Rove", provider: "fake", model: "fake", status: "idle", profile: "", workspaceId: "" }];
      case "session.list":
        return [];
      case "session.history":
        return [];
      case "session.send":
        return new Promise<void>((resolve) => { releaseSend = resolve; });
      default:
        return null;
    }
  }),
  subscribeEvents: vi.fn((filter: string, h: Handler) => {
    if (!handlers.has(filter)) handlers.set(filter, new Set());
    handlers.get(filter)!.add(h);
    return () => handlers.get(filter)?.delete(h);
  }),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
  pickFolder: vi.fn(),
}));

vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "en", theme: "graphite" }) }));

import { Chat } from "../app/Chat";

const sess = (id: string): Session => ({ id, title: id, agentId: "a1", workspaceId: "w", updatedAt: "" });
const emit = (sid: string, type: string, payload: Record<string, unknown>) =>
  act(() => handlers.get(`session.${sid}`)?.forEach((h) => h({ type, topic: `session.${sid}`, payload: { sessionId: sid, ...payload } })));
const stopShown = () => screen.queryAllByRole("button", { name: /stop/i }).length > 0;

describe("chat run state is per session", () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = () => {}; // jsdom has no layout
    handlers.clear();
    releaseSend = null;
  });
  afterEach(cleanup);

  // A reply arrives a few characters at a time. Following it to the foot is
  // right only while you are already there; otherwise the view was yanked
  // back down every few milliseconds and juddered.
  it("a reply still writing does not drag the view back down", async () => {
    render(<Chat session={sess("A")} workspaceId="w" onNewDraft={() => {}} />);
    const box = await screen.findByPlaceholderText(/message/i);
    await waitFor(() => expect((box as HTMLTextAreaElement).disabled).toBe(false));
    const view = document.querySelector(".thread-viewport") as HTMLElement;
    // jsdom lays nothing out: give the view a size and a long transcript
    let top = 0;
    Object.defineProperty(view, "clientHeight", { value: 500, configurable: true });
    Object.defineProperty(view, "scrollHeight", { get: () => 5000, configurable: true });
    Object.defineProperty(view, "scrollTop", { get: () => top, set: (v: number) => { top = v; }, configurable: true });

    top = 4500; // at the foot to begin with
    fireEvent.scroll(view);
    top = 1000; // then pulled up to read something
    fireEvent.scroll(view);
    emit("A", "message.delta", { delta: "bir " });
    emit("A", "message.delta", { delta: "iki " });
    expect(top).toBe(1000);

    // text arriving moves the foot away without moving the view: still held
    Object.defineProperty(view, "scrollHeight", { get: () => 6000, configurable: true });
    emit("A", "message.delta", { delta: "dört " });
    expect(top).toBe(1000);

    top = 5500; // back at the foot
    fireEvent.scroll(view);
    emit("A", "message.delta", { delta: "üç" });
    expect(top).toBe(6000);
  });

  it("does not carry another session's thinking into this one or a draft", async () => {
    const onCreate = vi.fn();
    const { rerender } = render(<Chat session={sess("A")} workspaceId="w" onCreateSession={onCreate} onNewDraft={() => {}} />);
    const box = await screen.findByPlaceholderText(/message/i);
    await waitFor(() => expect((box as HTMLTextAreaElement).disabled).toBe(false));

    fireEvent.change(box, { target: { value: "build the login page" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(stopShown()).toBe(true)); // A is thinking
    emit("A", "message.delta", { delta: "working on A…" });
    expect(screen.getByText(/working on A/)).toBeTruthy();

    // switch to B while A's reply is still running
    rerender(<Chat session={sess("B")} workspaceId="w" onCreateSession={onCreate} onNewDraft={() => {}} />);
    await waitFor(() => expect(stopShown()).toBe(false));
    expect(screen.queryByText(/working on A/)).toBeNull();
    expect(screen.queryByText(/thinking/i)).toBeNull();
    // A's events do not reach B
    emit("A", "message.delta", { delta: " more A" });
    expect(screen.queryByText(/more A/)).toBeNull();
    // and B can send while A is busy
    fireEvent.change(screen.getByPlaceholderText(/message/i), { target: { value: "x" } });
    expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(false);

    // a new draft is clean too
    rerender(<Chat session={null} draft workspaceId="w" onCreateSession={onCreate} onNewDraft={() => {}} />);
    await waitFor(() => expect(stopShown()).toBe(false));
    expect(screen.queryByText(/working on A/)).toBeNull();

    // back on A, its run is still shown as running
    rerender(<Chat session={sess("A")} workspaceId="w" onCreateSession={onCreate} onNewDraft={() => {}} />);
    await waitFor(() => expect(stopShown()).toBe(true));

    await act(async () => { releaseSend?.(); });
    await waitFor(() => expect(stopShown()).toBe(false));
  });
});
