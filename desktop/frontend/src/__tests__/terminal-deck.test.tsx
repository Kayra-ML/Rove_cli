import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Session } from "~/lib/types";

// Terminal mode: one session split into terminals. The session is T1, the
// others are its terminals (children), and the layout is kept with it.

const calls: [string, Record<string, unknown>][] = [];
const parent: Session = { id: "P", title: "Giriş sayfası", agentId: "a", workspaceId: "w", updatedAt: "", space: "chat" };
let saved: unknown = null;
let panesList = [
  { n: 1, id: "P", label: "T1", title: "Giriş sayfası", running: false },
  { n: 2, id: "C2", label: "T2", title: "Terminal 2", role: "Frontend Expert", running: true, task: "butonları yap" },
];
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: Record<string, unknown> = {}) => {
    calls.push([method, params]);
    switch (method) {
      case "terminal.layout.get": return saved;
      case "terminal.layout.set": saved = params.layout; return { ok: true };
      case "terminal.panes": return panesList;
      case "terminal.list": return [];
      case "terminal.newPane": {
        const child = { id: "C3", title: "Terminal 3", agentId: "a", workspaceId: "w", parentId: params.parentId, updatedAt: "" };
        panesList = [...panesList, { n: 3, id: "C3", label: "T3", title: "Terminal 3", running: false }];
        return child;
      }
      default: return null;
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
}));
vi.mock("../hooks/useApi", () => ({
  useAgents: () => ({ agents: [{ id: "a", name: "A" }] }),
  useWorkspaces: () => ({ workspaces: [{ id: "w", name: "proj", path: "/p" }] }),
}));
vi.mock("../hooks/usePersona", () => ({
  usePersonaBadges: () => ({ badgeFor: () => null }),
  useCharacterName: () => (_id: string, fallback: string) => fallback,
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr" }) }));
// the conversation itself is tested elsewhere: here it only shows what it got
vi.mock("../app/Chat", () => ({
  Chat: (p: { session: Session | null; draft?: boolean; pane?: { label: string }; onCreateSession?: () => Promise<Session>; onSession?: (s: Session) => void; onNewDraft?: () => void }) => (
    <div className="fake-chat" data-session={p.session?.id ?? ""} data-draft={String(Boolean(p.draft))} data-label={p.pane?.label ?? ""}>
      {p.onCreateSession && <button type="button" onClick={() => void p.onCreateSession!()}>ilk mesaj</button>}
      {p.onSession && <button type="button" onClick={() => p.onSession!({ ...parent, id: "OTHER" })}>/session</button>}
      {p.onNewDraft && <button type="button" onClick={() => p.onNewDraft!()}>/new</button>}
    </div>
  ),
}));
vi.mock("../app/Terminal", () => ({ Terminal: () => <div className="fake-shell" /> }));

import { splitFor, TerminalDeck } from "../app/TerminalDeck";

const chats = () => Array.from(document.querySelectorAll(".fake-chat")) as HTMLElement[];
const shown = () => chats().map((c) => c.dataset.session || (c.dataset.draft === "true" ? "draft" : "?"));
const labels = () => Array.from(document.querySelectorAll(".deck > .pane")).map((p) => (p as HTMLElement).dataset.pane);

describe("terminal deck", () => {
  beforeEach(() => {
    calls.length = 0;
    localStorage.clear();
    saved = null;
    panesList = [
      { n: 1, id: "P", label: "T1", title: "Giriş sayfası", running: false },
      { n: 2, id: "C2", label: "T2", title: "Terminal 2", role: "Frontend Expert", running: true, task: "butonları yap" },
    ];
  });
  afterEach(cleanup);

  it("picks the smallest split that holds the panes", () => {
    expect([0, 1, 2, 3, 4, 5, 7, 8, 9].map(splitFor)).toEqual([1, 1, 2, 4, 4, 6, 8, 8, 8]);
  });

  it("a session opens with its terminals as it was left: T1 is the session", async () => {
    saved = { split: 4, panes: [{ kind: "chat", sessionId: "P" }, { kind: "chat", sessionId: "C2" }, { kind: "shell", termId: "gone" }] };
    render(<TerminalDeck activeSession={parent} onFocusSession={vi.fn()} />);
    await waitFor(() => expect(shown()).toEqual(["P", "C2", "draft", "draft"]));
    expect(labels().slice(0, 2)).toEqual(["T1", "T2"]);
    expect(chats()[1].dataset.label).toBe("T2");
    // its name and task show in T2's header — no character; T1 cannot be closed
    const t2 = document.querySelectorAll(".deck > .pane")[1] as HTMLElement;
    expect(t2.textContent).toContain("Terminal 2");
    expect(t2.textContent).not.toContain("Frontend Expert");
    expect(t2.textContent).toContain("butonları yap");
    expect((document.querySelectorAll(".deck > .pane")[0] as HTMLElement).querySelector('button[title="Kapat"]')).toBeNull();
    expect(document.querySelector(".deck-title")?.textContent).toBe("Giriş sayfası");
  });

  it("a new terminal's first message opens it under the session; the layout is kept", async () => {
    const create = vi.fn(async () => parent);
    localStorage.setItem("aether.term.split", "2");
    const { rerender } = render(<TerminalDeck activeSession={null} draft onFocusSession={vi.fn()} onCreateSession={create} />);
    await waitFor(() => expect(shown()).toEqual(["draft", "draft"]));
    // writing in the second terminal first: the session is made, then its terminal
    await act(async () => { fireEvent.click(within(chats()[1]).getByText("ilk mesaj")); });
    expect(create).toHaveBeenCalledTimes(1);
    expect(calls.find(([m]) => m === "terminal.newPane")?.[1]).toEqual({ parentId: "P" });
    rerender(<TerminalDeck activeSession={parent} onFocusSession={vi.fn()} onCreateSession={create} />);
    await waitFor(() => expect(shown()).toEqual(["P", "C3"]));
    await waitFor(() => expect(saved).toEqual({ split: 2, panes: [{ kind: "chat", sessionId: "P" }, { kind: "chat", sessionId: "C3" }] }));
    expect(calls.filter(([m]) => m === "session.create")).toHaveLength(0); // no chat of its own
  });

  it("a closed terminal stays in the session and reopens from +", async () => {
    saved = { split: 2, panes: [{ kind: "chat", sessionId: "P" }, { kind: "chat", sessionId: "C2" }] };
    render(<TerminalDeck activeSession={parent} onFocusSession={vi.fn()} />);
    await waitFor(() => expect(shown()).toEqual(["P", "C2"]));
    const t2 = () => document.querySelectorAll(".deck > .pane")[1] as HTMLElement;
    fireEvent.click(t2().querySelector('button[title="Kapat"]')!);
    await waitFor(() => expect(shown()).toEqual(["P", "draft"]));
    fireEvent.click((document.querySelectorAll(".deck > .pane")[0] as HTMLElement).querySelector('button[title="Yan yana aç"]')!);
    const reopen = await screen.findByText("Terminal 2");
    fireEvent.click(reopen.closest("button")!);
    await waitFor(() => expect(shown()).toEqual(["P", "C2"]));
  });

  it("/new in T1 starts a new session; /session switches the whole deck", async () => {
    const onNew = vi.fn();
    const onFocus = vi.fn();
    render(<TerminalDeck activeSession={parent} onFocusSession={onFocus} onNewSession={onNew} />);
    await waitFor(() => expect(shown()[0]).toBe("P"));
    fireEvent.click(within(chats()[0]).getByText("/new"));
    expect(onNew).toHaveBeenCalled();
    fireEvent.click(within(chats()[0]).getByText("/session"));
    expect(onFocus).toHaveBeenCalledWith(expect.objectContaining({ id: "OTHER" }));
  });

  it("the split is picked at the top right and saved with the session", async () => {
    render(<TerminalDeck activeSession={parent} onFocusSession={vi.fn()} />);
    await waitFor(() => expect(shown()[0]).toBe("P"));
    const picker = screen.getByRole("radiogroup", { name: "Bölme" });
    expect(Array.from(picker.querySelectorAll("button")).map((b) => b.textContent)).toEqual(["1", "2", "4", "6", "8"]);
    fireEvent.click(screen.getByRole("radio", { name: /4/ }));
    await waitFor(() => expect(chats()).toHaveLength(4));
    await waitFor(() => expect((saved as { split: number } | null)?.split).toBe(4));
    fireEvent.click(screen.getByRole("radio", { name: /^1/ }));
    expect(document.querySelectorAll(".deck > .pane")).toHaveLength(1);
    expect(shown()).toEqual(["P"]);
  });
});
