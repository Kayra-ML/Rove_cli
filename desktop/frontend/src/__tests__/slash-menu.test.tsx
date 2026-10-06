import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Session } from "../lib/types";

// The "/" menu scrolls (wheel or arrows keep the highlighted row in view),
// and /models opens a picker for the chat's model.

const calls: [string, unknown][] = [];
let chatModel = { provider: "openai", model: "gpt-4o", source: "agent" };
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    switch (method) {
      case "agent.list":
        return [{ id: "a1", name: "Rove", provider: "openai", model: "gpt-4o", status: "idle", profile: "", workspaceId: "" }];
      case "session.list":
        return [
          { id: "S", title: "S", agentId: "a1", workspaceId: "w", updatedAt: "2026-09-27T10:00:00.000000000Z", space: "chat" },
          { id: "OLD", title: "Eski giriş sayfası", agentId: "a1", workspaceId: "w", updatedAt: "2026-09-20T10:00:00.000000000Z", space: "chat" },
          { id: "NEW", title: "Ödeme hatası", agentId: "a1", workspaceId: "w", updatedAt: "2026-09-27T12:00:00.000000000Z", space: "chat" },
          { id: "OFF", title: "Ofis sohbeti", agentId: "a1", workspaceId: "w", updatedAt: "2026-09-27T13:00:00.000000000Z", space: "office" },
        ];
      case "session.history":
        return [];
      case "model.list":
        return [
          { provider: "openai", model: "gpt-4o", default: true },
          { provider: "openai", model: "gpt-4o-mini", default: true },
          { provider: "anthropic", model: "claude-sonnet-5" },
        ];
      case "session.model":
        return chatModel;
      case "session.setModel": {
        const p = params as { provider: string; model: string };
        chatModel = p.model ? { provider: p.provider, model: p.model, source: "chat" } : { provider: "openai", model: "gpt-4o", source: "agent" };
        return { ok: true };
      }
      default:
        return null;
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
  pickFolder: vi.fn(),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { Chat } from "../app/Chat";
import { filterSlash } from "../lib/slash";

// a chat's menu: every command but the terminal's
const SLASH = filterSlash("/", "chat");

const sess: Session = { id: "S", title: "S", agentId: "a1", workspaceId: "w", updatedAt: "" };
const menu = () => document.querySelector(".slash-menu") as HTMLElement | null;
const active = () => menu()?.querySelector(".slash-row.active")?.textContent ?? "";

describe("slash menu", () => {
  const scrolled: string[] = [];
  beforeEach(() => {
    calls.length = 0;
    scrolled.length = 0;
    chatModel = { provider: "openai", model: "gpt-4o", source: "agent" };
    Element.prototype.scrollIntoView = function (this: Element) { scrolled.push(this.textContent ?? ""); };
  });
  afterEach(cleanup);

  const open = async (onSession: (s: Session) => void = () => {}, onNewDraft: () => void = () => {}) => {
    render(<Chat session={{ ...sess, space: "chat" }} workspaceId="w" onNewDraft={onNewDraft} onSession={onSession} />);
    const box = (await screen.findByPlaceholderText("Mesaj…")) as HTMLTextAreaElement;
    await waitFor(() => expect(box.disabled).toBe(false));
    return box;
  };

  it("is a scrolling list the arrow keys walk through, keeping the highlight in view", async () => {
    const box = await open();
    fireEvent.change(box, { target: { value: "/" } });
    expect(menu()?.querySelectorAll(".slash-row")).toHaveLength(SLASH.length);
    expect(active()).toContain("/models");
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "ArrowDown" });
    expect(active()).toContain(`/${SLASH[2].id}`);
    expect(scrolled.at(-1)).toContain(`/${SLASH[2].id}`);
    // up from the top wraps to the last command, which scrolls into view
    fireEvent.keyDown(box, { key: "ArrowUp" });
    fireEvent.keyDown(box, { key: "ArrowUp" });
    fireEvent.keyDown(box, { key: "ArrowUp" });
    expect(active()).toContain(`/${SLASH[SLASH.length - 1].id}`);
    expect(scrolled.at(-1)).toContain(`/${SLASH[SLASH.length - 1].id}`);
    // a mouse resting on a row that scrolls under it does not steal the highlight
    fireEvent.mouseEnter(menu()!.querySelectorAll(".slash-row")[0]);
    expect(active()).toContain(`/${SLASH[SLASH.length - 1].id}`);
    // Enter fills the command in
    fireEvent.keyDown(box, { key: "Enter" });
    expect(box.value).toBe(`/${SLASH[SLASH.length - 1].id} `);
  });

  it("/models opens a picker; arrows and Enter set this chat's model", async () => {
    const box = await open();
    fireEvent.change(box, { target: { value: "/models" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(menu()?.getAttribute("aria-label")).toBe("Bu sohbetin modelini seç"));
    expect(menu()!.textContent).toContain("şu an: gpt-4o");
    expect(Array.from(menu()!.querySelectorAll(".slash-row code")).map((c) => c.textContent)).toEqual(["gpt-4o", "gpt-4o-mini", "claude-sonnet-5"]);
    expect(active()).toContain("gpt-4o"); // starts on the current one
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "ArrowDown" });
    expect(active()).toContain("claude-sonnet-5");
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(calls.find(([m]) => m === "session.setModel")?.[1]).toEqual({ sessionId: "S", provider: "anthropic", model: "claude-sonnet-5" }));
    await waitFor(() => expect(menu()).toBeNull());
    expect(box.value).toBe("");

    // typing filters; Escape closes without changing anything
    fireEvent.change(box, { target: { value: "/models" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(menu()).toBeTruthy());
    // the chat has its own model now, so "back to default" leads the list
    expect(menu()!.querySelector(".slash-row code")?.textContent).toContain("Varsayılana dön");
    fireEvent.change(box, { target: { value: "mini" } });
    expect(Array.from(menu()!.querySelectorAll(".slash-row code")).map((c) => c.textContent)).toEqual(["↺ Varsayılana dön", "gpt-4o-mini"]);
    fireEvent.keyDown(box, { key: "Escape" });
    expect(menu()).toBeNull();
    expect(calls.filter(([m]) => m === "session.setModel")).toHaveLength(1);
  });

  // From the model button the menu has its own search: it narrows the list
  // in place (the menu keeps its size) and Enter takes the highlighted one.
  it("the model button's menu searches as you type", async () => {
    await open();
    fireEvent.click(document.querySelector(".model-pill") as HTMLElement);
    const search = await waitFor(() => { const el = document.querySelector(".model-search input") as HTMLInputElement | null; expect(el).toBeTruthy(); return el!; });
    fireEvent.change(search, { target: { value: "sonnet" } });
    expect(Array.from(menu()!.querySelectorAll(".slash-row code")).map((c) => c.textContent).filter((x) => !x?.includes("Varsayılana"))).toEqual(["claude-sonnet-5"]);
    fireEvent.keyDown(search, { key: "ArrowDown" });
    fireEvent.keyDown(search, { key: "Enter" });
    await waitFor(() => expect(calls.filter(([m]) => m === "session.setModel").pop()?.[1]).toEqual({ sessionId: "S", provider: "anthropic", model: "claude-sonnet-5" }));
    await waitFor(() => expect(menu()).toBeNull());
  });

  it("/models <name> sets it straight away when the name is clear", async () => {
    const box = await open();
    fireEvent.change(box, { target: { value: "/models sonnet" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(calls.find(([m]) => m === "session.setModel")?.[1]).toEqual({ sessionId: "S", provider: "anthropic", model: "claude-sonnet-5" }));
    expect(menu()).toBeNull();
    // nothing was sent to the model as a message
    expect(calls.some(([m]) => m === "session.send")).toBe(false);
  });

  it("the model button shows the chat's model and opens the picker by it, keeping what was typed", async () => {
    const box = await open();
    const pill = await screen.findByRole("button", { name: "Model seç" });
    await waitFor(() => expect(pill.textContent).toContain("gpt-4o"));
    expect(pill.textContent).toContain("openai");
    fireEvent.change(box, { target: { value: "merhaba dünya" } });
    fireEvent.click(pill);
    await waitFor(() => expect(menu()?.className).toContain("from-pill"));
    expect(box.value).toBe(""); // the box filters models now
    // grouped by provider
    expect(Array.from(menu()!.querySelectorAll(".model-group")).map((g) => g.textContent)).toEqual(["openai · varsayılan", "anthropic"]);
    expect(menu()!.querySelector(".slash-row.current code")?.textContent).toBe("gpt-4o");
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(calls.find(([m]) => m === "session.setModel")?.[1]).toEqual({ sessionId: "S", provider: "anthropic", model: "claude-sonnet-5" }));
    // the message comes back, and the button shows the new model
    await waitFor(() => expect(box.value).toBe("merhaba dünya"));
    await waitFor(() => expect(pill.textContent).toContain("claude-sonnet-5"));
    // clicking the button again (or outside) closes without changing anything
    fireEvent.click(pill);
    await waitFor(() => expect(menu()).toBeTruthy());
    fireEvent.click(pill);
    await waitFor(() => expect(menu()).toBeNull());
    expect(box.value).toBe("merhaba dünya");
    expect(calls.filter(([m]) => m === "session.setModel")).toHaveLength(1);
  });

  it("in the terminal, /models opens the same picker", async () => {
    render(<Chat variant="terminal" session={sess} workspaceId="w" onNewDraft={() => {}} />);
    const box = (await waitFor(() => {
      const ta = document.querySelector(".term-textarea") as HTMLTextAreaElement | null;
      expect(ta?.disabled).toBe(false);
      return ta!;
    }));
    await waitFor(() => expect(document.querySelector(".term-model")?.textContent).toContain("gpt-4o"));
    fireEvent.change(box, { target: { value: "/models" } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(menu()?.getAttribute("aria-label")).toBe("Bu sohbetin modelini seç"));
    fireEvent.change(box, { target: { value: "mini" } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(calls.find(([m]) => m === "session.setModel")?.[1]).toEqual({ sessionId: "S", provider: "openai", model: "gpt-4o-mini" }));
    await waitFor(() => expect(document.querySelector(".term-model")?.textContent).toContain("gpt-4o-mini"));
    // and the model in the terminal's status line opens it too
    fireEvent.click(document.querySelector(".term-model")!);
    await waitFor(() => expect(menu()?.className).toContain("from-pill"));
  });

  it("/session lists this space's chats, newest first, and switches to the one picked", async () => {
    const onSession = vi.fn();
    const box = await open(onSession);
    fireEvent.change(box, { target: { value: "/session" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(menu()?.getAttribute("aria-label")).toBe("Oturumlar"));
    // Office's chat is not listed; newest first; the open chat is marked
    expect(Array.from(menu()!.querySelectorAll(".session-title")).map((x) => x.textContent)).toEqual(["Ödeme hatası", "S", "Eski giriş sayfası"]);
    expect(menu()!.querySelector(".slash-row.current .session-title")?.textContent).toBe("S");
    expect(active()).toContain("S"); // starts on the open chat
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSession).toHaveBeenCalledWith(expect.objectContaining({ id: "OLD" })));
    expect(menu()).toBeNull();
    expect(box.value).toBe("");
  });

  it("/session <search> switches straight away when one chat matches, and filters otherwise", async () => {
    const onSession = vi.fn();
    const box = await open(onSession);
    fireEvent.change(box, { target: { value: "/session ödeme" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(onSession).toHaveBeenCalledWith(expect.objectContaining({ id: "NEW" })));
    expect(menu()).toBeNull();
    fireEvent.change(box, { target: { value: "/oturum s" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(menu()?.getAttribute("aria-label")).toBe("Oturumlar"));
    // typing narrows it; Escape closes without switching
    fireEvent.change(box, { target: { value: "giriş" } });
    expect(Array.from(menu()!.querySelectorAll(".session-title")).map((x) => x.textContent)).toEqual(["Eski giriş sayfası"]);
    fireEvent.keyDown(box, { key: "Escape" });
    expect(menu()).toBeNull();
    expect(onSession).toHaveBeenCalledTimes(1);
    expect(calls.some(([m]) => m === "session.send")).toBe(false);
  });

  it("/new opens a new chat as a draft; nothing is created", async () => {
    const onNewDraft = vi.fn();
    const box = await open(() => {}, onNewDraft);
    fireEvent.change(box, { target: { value: "/new" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(onNewDraft).toHaveBeenCalledTimes(1));
    expect(calls.some(([m]) => m === "session.create")).toBe(false);
  });

  it("caps the menu's height so long lists scroll", () => {
    const css = readFileSync(resolve(__dirname, "../styles/global.css"), "utf8");
    const rule = css.slice(css.indexOf(".slash-menu {"), css.indexOf("}", css.indexOf(".slash-menu {")));
    expect(rule).toMatch(/max-height:/);
    expect(rule).toMatch(/overflow-y: auto/);
    expect(rule).not.toMatch(/overflow: hidden/);
  });
});
