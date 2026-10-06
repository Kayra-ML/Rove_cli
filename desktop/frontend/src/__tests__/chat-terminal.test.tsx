import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Session } from "../lib/types";

// Commands of a terminal that is one of a session's terminals: /panes,
// /to and @T2, /all, /stop 2, and /character for roles.

const calls: [string, Record<string, unknown>][] = [];
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: Record<string, unknown> = {}) => {
    calls.push([method, params]);
    switch (method) {
      case "agent.list": return [{ id: "a1", name: "Rove", provider: "openai", model: "gpt-4o", status: "idle", profile: "", workspaceId: "" }];
      case "session.list":
      case "session.history": return [];
      case "terminal.panes": return [
        { n: 1, id: "P", label: "T1", title: "Giriş", role: "Go Backend Expert", running: false, task: "API yaz" },
        { n: 2, id: "C2", label: "T2", title: "T2", role: "Frontend Expert", running: true, task: "butonlar" },
        { n: 3, id: "C3", label: "T3", title: "T3", running: false },
      ];
      case "terminal.sendPane": {
        if (params.to === "T2" && String(params.message).includes("meşgul")) throw new Error("T2 (Frontend Expert) is busy; send it when it is done");
        return { n: 2, id: "C2", label: String(params.to).toUpperCase().startsWith("T") ? String(params.to).toUpperCase() : "T2", role: "Frontend Expert" };
      }
      case "persona.catalog": return { characters: [
        { id: "frontend", name: "Frontend Uzmanı", category: "Yazılım", summary: "arayüz", role: "frontend", features: [], prompt: "" },
        { id: "go-backend", name: "Go Backend Uzmanı", category: "Yazılım", summary: "api", role: "backend", features: [], prompt: "" },
      ], features: [], defaults: [] };
      default: return null;
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
  pickFolder: vi.fn(),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { Chat } from "../app/Chat";

const sess: Session = { id: "P", title: "Giriş", agentId: "a1", workspaceId: "w", updatedAt: "" };
const of = (m: string) => calls.filter(([x]) => x === m).map(([, p]) => p);

async function openTerminal() {
  render(<Chat variant="terminal" session={sess} workspaceId="w" pane={{ label: "T1", groupId: "P" }} onNewDraft={() => {}} />);
  const box = await waitFor(() => {
    const ta = document.querySelector(".term-textarea") as HTMLTextAreaElement | null;
    expect(ta?.disabled).toBe(false);
    return ta!;
  });
  return box;
}
const run = (box: HTMLTextAreaElement, text: string) => {
  fireEvent.change(box, { target: { value: text } });
  fireEvent.submit(box.closest("form")!);
};

describe("terminal commands", () => {
  beforeEach(() => { calls.length = 0; Element.prototype.scrollIntoView = () => {}; });
  afterEach(cleanup);

  it("/panes shows who is doing what", async () => {
    const box = await openTerminal();
    run(box, "/panes");
    const note = await screen.findByRole("note");
    expect(note.textContent).toContain("T1 ◂ · boşta — API yaz");
    expect(note.textContent).toContain("T2 · çalışıyor — butonlar");
    expect(note.textContent).toContain("T3 · boşta");
    expect(note.textContent).not.toContain("Expert");
  });

  it("@T2 and /to hand another terminal work; nothing is sent to this one", async () => {
    const box = await openTerminal();
    run(box, "@T2 butonları yuvarla");
    await waitFor(() => expect(of("terminal.sendPane")[0]).toEqual({ from: "P", to: "T2", message: "butonları yuvarla" }));
    run(box, "/to frontend testleri yaz");
    await waitFor(() => expect(of("terminal.sendPane")[1]).toEqual({ from: "P", to: "frontend", message: "testleri yaz" }));
    expect(of("session.send")).toHaveLength(0);
    // a busy terminal says so
    run(box, "/to T2 meşgul misin");
    await waitFor(() => expect(document.querySelector(".term-err")?.textContent).toContain("busy"));
  });

  it("/all gives every other terminal the work; /stop 2 stops T2", async () => {
    const box = await openTerminal();
    run(box, "/all lint çalıştır");
    await waitFor(() => expect(of("terminal.sendPane").map((p) => p.to)).toEqual(["T2", "T3"]));
    run(box, "/stop 2");
    await waitFor(() => expect(of("session.cancel")).toEqual([{ sessionId: "C2" }]));
  });

  // terminals have no characters: /character is not a command, and no
  // agent name sits under the box
  it("has no /character and no character under the box", async () => {
    const box = await openTerminal();
    fireEvent.change(box, { target: { value: "/charac" } });
    expect(Array.from(document.querySelectorAll(".slash-menu [role=option], .slash-menu .slash-row")).some((o) => o.textContent?.includes("/character"))).toBe(false);
    expect(document.querySelector(".term-mode-agent")).toBeNull();
  });

  // the agent's stored default (gpt-4o here) is offered by no connected
  // account: the terminal says no model is picked rather than naming it
  it("does not name a model no account offers", async () => {
    await openTerminal();
    await waitFor(() => expect(document.querySelector(".term-model")?.textContent).toContain("model seç"));
    expect(document.querySelector(".term")?.textContent).toContain("model seçilmedi");
    expect(document.querySelector(".term")?.textContent).not.toContain("gpt-4o");
  });

  it("in a chat (not a terminal) these are not commands", async () => {
    render(<Chat session={sess} workspaceId="w" onNewDraft={() => {}} />);
    const box = (await screen.findByPlaceholderText("Mesaj…")) as HTMLTextAreaElement;
    await waitFor(() => expect(box.disabled).toBe(false));
    fireEvent.change(box, { target: { value: "/pa" } });
    expect(document.querySelector(".slash-menu")).toBeNull();
    fireEvent.change(box, { target: { value: "/character go" } });
    expect(document.querySelector(".slash-menu")).toBeNull();
  });
});
