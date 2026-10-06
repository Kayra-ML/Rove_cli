import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { CARD_H, CARD_W } from "~/lib/cables";

const calls: [string, unknown][] = [];
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    switch (method) {
      case "session.list": return [
        { id: "A", title: "Web", agentId: "a", workspaceId: "w", updatedAt: "", space: "chat" },
        { id: "B", title: "Masaüstü", agentId: "a", workspaceId: "w", updatedAt: "", space: "chat" },
      ];
      case "ctxmap.get": return { nodes: [{ sessionId: "A", x: 0, y: 0 }, { sessionId: "B", x: 500, y: 0 }, ...agentNodes], links: [] };
      case "ctxmap.link": return { id: "L1", sessionA: "A", sessionB: "B", direction: "both", auto: true };
      case "ctxmap.updateLink": return { id: "L1", sessionA: "A", sessionB: "B", direction: "both", auto: true, ...(params as object) };
      case "staff.watches": return watches;
      case "staff.handoffs": return handoffs;
      case "staff.handoffSave": {
        const h = { id: "H1", instruction: "", dailyLimit: 10, enabled: true, ...(params as object) };
        handoffs = [h];
        return h;
      }
      case "staff.watchSave": {
        const w = { id: "W1", instruction: "", dailyLimit: 10, enabled: true, ...(params as object) };
        watches = [w];
        return w;
      }
      default: return [];
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));
vi.mock("../hooks/useApi", () => ({
  useWorkspaces: () => ({ workspaces: [], reload: vi.fn() }),
  useProfiles: () => ({ profiles: [
    { id: "P1", name: "Ayşe", title: "Test uzmanı", role: "qa", systemPrompt: "", model: "", provider: "", isDefault: false, isLeader: false },
    { id: "P2", name: "Ali", title: "Backend", role: "backend", systemPrompt: "", model: "", provider: "", isDefault: false, isLeader: false },
  ], reload: vi.fn() }),
}));
vi.mock("../hooks/useStaff", () => ({ useStaff: () => ({ members: [], reload: vi.fn() }) }));
let agentNodes: { sessionId: string; x: number; y: number }[] = [];
let watches: unknown[] = [];
let handoffs: unknown[] = [];
// the Masaüstü chat is held with an Agent-space agent
vi.mock("../hooks/usePersona", () => ({
  usePersonaBadges: () => ({ badgeFor: (id: string) => (id === "B" ? { name: "Ayşe", profileId: "P1", mark: "squircle:prompt", color: "#3987e5" } : null) }),
}));

import { ContextMap } from "../app/ContextMap";

// jsdom has no PointerEvent: without one, a pointer event carries no
// coordinates or keys. A MouseEvent with a pointer id is all the map reads.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventShim extends MouseEvent {
    pointerId: number;
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init);
      this.pointerId = init.pointerId ?? 1;
    }
  }
  (window as unknown as { PointerEvent: unknown }).PointerEvent = PointerEventShim;
}

// The camera starts at (80, 80), scale 1, and the stage sits at the
// window's corner: a card at world (x, y) is at screen (x + 80, y + 80).
const at = (x: number, y: number) => ({ clientX: x + 80, clientY: y + 80, button: 0, pointerId: 1 });

describe("Session Map: drawing a cable", () => {
  beforeEach(() => { calls.length = 0; agentNodes = []; watches = []; handoffs = []; try { localStorage.clear(); } catch { /* none */ } });
  afterEach(cleanup);

  // the cards by their titles: Web stands at (0, 0), Masaüstü at (500, 0)
  const cards = async () => {
    await waitFor(() => expect(document.querySelectorAll(".cmap-card")).toHaveLength(2));
    const by = (title: string) => Array.from(document.querySelectorAll(".cmap-card")).find((c) => c.querySelector(".cmap-card-title")?.textContent === title) as HTMLElement;
    return [by("Web"), by("Masaüstü")];
  };

  it("starts anywhere on a card's edge and lands on the card it is dropped on", async () => {
    render(<ContextMap />);
    const [a] = await cards();
    // near the edge the card offers a cable
    fireEvent.pointerMove(a, at(CARD_W - 4, CARD_H / 2));
    expect(a.className).toContain("edge");
    fireEvent.pointerMove(a, at(CARD_W / 2, CARD_H / 2));
    expect(a.className).not.toContain("edge");
    // pressing at the bottom edge, far from the ports, starts one
    fireEvent.pointerDown(a, at(CARD_W / 2, CARD_H - 3));
    const s = a.closest(".cmap-world")!.parentElement as HTMLElement;
    fireEvent.pointerMove(s, at(560, 40));
    // the draft is a straight line, and the card under it lights up
    await waitFor(() => expect(document.querySelector(".cmap-wire-draft")?.getAttribute("d")).toMatch(/^M [\d.]+ [\d.]+ L 560 40$/));
    expect((await cards())[1].className).toContain("drop");
    await act(async () => { fireEvent.pointerUp(s, at(560, 40)); });
    await waitFor(() => expect(calls.find(([m]) => m === "ctxmap.link")?.[1]).toEqual({ sessionA: "A", sessionB: "B" }));
    // the card did not move
    expect(calls.some(([m]) => m === "ctxmap.place")).toBe(false);
  });

  it("with ⌥ held a cable starts from the middle of a card; without it the card moves", async () => {
    render(<ContextMap />);
    const [a] = await cards();
    const s = a.closest(".cmap-world")!.parentElement as HTMLElement;
    fireEvent.pointerDown(a, { ...at(CARD_W / 2, CARD_H / 2), altKey: true });
    fireEvent.pointerMove(s, at(560, 40));
    await waitFor(() => expect(document.querySelector(".cmap-wire-draft")).toBeTruthy());
    await act(async () => { fireEvent.pointerUp(s, at(560, 40)); });
    await waitFor(() => expect(calls.some(([m]) => m === "ctxmap.link")).toBe(true));

    calls.length = 0;
    fireEvent.pointerDown(a, at(CARD_W / 2, CARD_H / 2));
    fireEvent.pointerMove(s, at(CARD_W / 2 + 40, CARD_H / 2 + 200));
    await act(async () => { fireEvent.pointerUp(s, at(CARD_W / 2 + 40, CARD_H / 2 + 200)); });
    await waitFor(() => expect(calls.some(([m]) => m === "ctxmap.place")).toBe(true));
    expect(calls.some(([m]) => m === "ctxmap.link")).toBe(false);
  });

  // An agent on the map watches a chat: the cable from the chat to the
  // agent is a watch, edited in its own panel, and goes with either card.
  it("a cable from a chat to an agent makes the agent watch the chat", async () => {
    agentNodes = [{ sessionId: "agent:P1", x: 0, y: 300 }];
    render(<ContextMap />);
    await waitFor(() => expect(document.querySelector('[data-agent="P1"]')).toBeTruthy());
    const agent = document.querySelector('[data-agent="P1"]') as HTMLElement;
    expect(agent.textContent).toContain("Ayşe");
    expect(agent.textContent).toContain("Test uzmanı");
    // drawn from the agent's edge to the chat: still the agent watches the chat
    const s = agent.closest(".cmap-world")!.parentElement as HTMLElement;
    fireEvent.pointerDown(agent, at(CARD_W / 2, 300 + 3));
    fireEvent.pointerMove(s, at(CARD_W / 2, CARD_H / 2));
    await act(async () => { fireEvent.pointerUp(s, at(CARD_W / 2, CARD_H / 2)); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.watchSave")?.[1]).toEqual({ sessionId: "A", profileId: "P1" }));
    expect(calls.some(([m]) => m === "ctxmap.link")).toBe(false);
    // the watch is drawn and its panel opens
    await waitFor(() => expect(document.querySelector('[data-watch="W1"]')).toBeTruthy());
    const panel = await waitFor(() => {
      const el = document.querySelector(".cmap-watch-panel") as HTMLElement | null;
      expect(el).toBeTruthy();
      return el!;
    });
    const ins = panel.querySelector("textarea")!;
    fireEvent.change(ins, { target: { value: "Testleri gözden geçir" } });
    await act(async () => { fireEvent.blur(ins); });
    await waitFor(() => expect(calls.filter(([m]) => m === "staff.watchSave").pop()?.[1]).toMatchObject({ id: "W1", instruction: "Testleri gözden geçir" }));
    // taking the agent off the map takes its watch with it
    await act(async () => { fireEvent.click(agent.querySelector(".cmap-card-x")!); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.watchDelete")?.[1]).toEqual({ id: "W1" }));
  });

  it("cards cannot be dragged into one another", async () => {
    render(<ContextMap />);
    const [a] = await cards();
    const s = a.closest(".cmap-world")!.parentElement as HTMLElement;
    // pull Web right, into Masaüstü at x 500
    fireEvent.pointerDown(a, at(CARD_W / 2, CARD_H / 2));
    fireEvent.pointerMove(s, at(CARD_W / 2 + 120, CARD_H / 2));
    fireEvent.pointerMove(s, at(CARD_W / 2 + 400, CARD_H / 2));
    await act(async () => { fireEvent.pointerUp(s, at(CARD_W / 2 + 400, CARD_H / 2)); });
    await waitFor(() => expect(calls.some(([m]) => m === "ctxmap.place")).toBe(true));
    const placed = calls.filter(([m]) => m === "ctxmap.place").pop()![1] as { x: number; y: number };
    // it stopped short of Masaüstü instead of landing on it
    expect(placed.x + CARD_W).toBeLessThanOrEqual(500);
  });

  it("a cable takes a colour, and an agent's chat wears the agent's logo", async () => {
    render(<ContextMap />);
    const [a] = await cards();
    // the agent's chat: its logo on the card and in the list, the agent's name under it
    expect((await cards())[1].querySelector(".agent-mark")).toBeTruthy();
    const row = Array.from(document.querySelectorAll(".cmap-item")).find((r) => r.textContent?.includes("Masaüstü"))!;
    expect(row.querySelector(".agent-mark")).toBeTruthy();
    expect(row.textContent).toContain("Ayşe");
    // draw a cable, then colour it
    const s = a.closest(".cmap-world")!.parentElement as HTMLElement;
    fireEvent.pointerDown(a, at(CARD_W - 3, CARD_H / 2));
    fireEvent.pointerMove(s, at(560, 40));
    await act(async () => { fireEvent.pointerUp(s, at(560, 40)); });
    const orange = await waitFor(() => {
      const b = document.querySelector('.cmap-swatches [aria-label="Turuncu"]') as HTMLElement | null;
      expect(b).toBeTruthy();
      return b!;
    });
    await act(async () => { fireEvent.click(orange); });
    await waitFor(() => expect(calls.find(([m]) => m === "ctxmap.updateLink")?.[1]).toEqual({ id: "L1", color: "orange" }));
    await waitFor(() => expect((document.querySelector(".cmap-wire-core") as SVGElement).style.stroke).toBe("var(--cc-orange)"));
  });

  it("a cable from one agent to another pairs them: the first's finished work goes on to the second", async () => {
    agentNodes = [{ sessionId: "agent:P2", x: 0, y: 300 }, { sessionId: "agent:P1", x: 500, y: 300 }];
    render(<ContextMap />);
    await waitFor(() => expect(document.querySelectorAll(".cmap-agent")).toHaveLength(2));
    const ali = document.querySelector('[data-agent="P2"]') as HTMLElement;
    const s = ali.closest(".cmap-world")!.parentElement as HTMLElement;
    fireEvent.pointerDown(ali, at(CARD_W - 3, 300 + CARD_H / 2));
    fireEvent.pointerMove(s, at(560, 340));
    await act(async () => { fireEvent.pointerUp(s, at(560, 340)); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.handoffSave")?.[1]).toEqual({ fromId: "P2", toId: "P1" }));
    await waitFor(() => expect(document.querySelector('[data-handoff="H1"]')).toBeTruthy());
    const panel = await waitFor(() => {
      const el = document.querySelector('.cmap-watch-panel[aria-label="Devretme"]') as HTMLElement | null;
      expect(el).toBeTruthy();
      return el!;
    });
    expect(panel.textContent).toContain("Ali → Ayşe");
    // taking Ali off the map takes the pairing with him
    await act(async () => { fireEvent.click(ali.querySelector(".cmap-card-x")!); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.handoffDelete")?.[1]).toEqual({ id: "H1" }));
  });
});
