import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { defaultMark, markOf, SHAPES, FACES } from "~/lib/agentmark";
import type { AgentProfile } from "~/lib/types";

describe("agent marks", () => {
  it("an agent keeps the same mark everywhere, and a stored one wins", () => {
    expect(defaultMark("abc")).toEqual(defaultMark("abc"));
    const m = markOf("hex:under", "#43b3a8", "abc");
    expect(m).toEqual({ shape: "hex", face: "under", color: "#43b3a8" });
    // unknown parts fall back to the seed's
    const d = defaultMark("abc");
    expect(markOf("blob:grin", "red", "abc")).toEqual(d);
    expect(SHAPES).toContain(d.shape);
    expect(FACES).toContain(d.face);
  });
  it("the shape follows the field; the shield is security's alone; the face is the logo's prompt", () => {
    expect(defaultMark("x", "security").shape).toBe("shield");
    expect(defaultMark("x", "frontend").shape).toBe("squircle");
    expect(defaultMark("x", "go-backend").shape).toBe("hex");
    for (let i = 0; i < 50; i++) expect(defaultMark(`agent-${i}`).shape).not.toBe("shield");
    expect(defaultMark("x", "qa").face).toBe("prompt");
    // a picked shape stays, whatever the character
    expect(markOf("drop:double", "#43b3a8", "x", "security").shape).toBe("drop");
  });
});

const calls: [string, unknown][] = [];
let profiles: AgentProfile[] = [];
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    switch (method) {
      case "profile.list": return profiles;
      case "profile.upsert": return { id: "new", ...(params as object) };
      case "model.list": return [{ provider: "Codex", model: "gpt-6-luna" }, { provider: "Kira", model: "claude-sonnet-5" }];
      case "session.list": return [{ id: "s1", title: "Ödeme hatası", agentId: "a", workspaceId: "w", updatedAt: new Date().toISOString(), space: "office" }];
      case "persona.badges": return { sessions: { s1: { name: "Ayşe", profileId: "p1", characterId: "frontend" } }, default: null };
      case "persona.catalog": return {
        characters: [{ id: "frontend", name: "Frontend Uzmanı", category: "Yazılım", summary: "UI", role: "frontend", features: ["read", "write"], prompt: "You are a senior frontend engineer." }],
        features: [{ key: "read", group: "tools", name: "Dosya okuma", desc: "" }, { key: "write", group: "tools", name: "Dosya yazma", desc: "" }, { key: "shell", group: "tools", name: "Terminal", desc: "" }],
        defaults: ["read", "write", "shell"],
      };
      case "usage.get": return null;
      default: return null;
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
  pickFolder: vi.fn(),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { OfficePanel } from "../app/OfficePanel";
import { SessionSidebar } from "../app/SessionSidebar";

const ayse: AgentProfile = { id: "p1", name: "Ayşe", role: "frontend", systemPrompt: "", model: "gpt-6-luna", provider: "Codex", isDefault: false, isLeader: false, characterId: "frontend", mark: "drop:dots", color: "#e5735f" };

describe("Settings → Office", () => {
  beforeEach(() => { calls.length = 0; profiles = []; localStorage.setItem("rove.office.v2", "1"); });
  afterEach(cleanup);

  it("an empty office says how to start", async () => {
    render(<OfficePanel />);
    await screen.findByText("Ekibin boş");
  });

  it("a new agent: logo, character, own prompt, a model from the list, its own permissions", async () => {
    render(<OfficePanel intent={{ mode: "new" }} />);
    fireEvent.change(await screen.findByLabelText("Ad"), { target: { value: "Ayşe" } });
    fireEvent.click(await screen.findByRole("button", { name: /Frontend Uzmanı/ }));
    fireEvent.click(screen.getByRole("radio", { name: "Kendi istemim" }));
    fireEvent.change(screen.getByPlaceholderText(/ödeme servisinden/), { target: { value: "Sen ödeme ekibindesin." } });
    const list = await screen.findByRole("listbox", { name: "Model" });
    await waitFor(() => expect(within(list).getByText("gpt-6-luna")).toBeTruthy());
    // the search narrows the list
    fireEvent.change(screen.getByPlaceholderText(/Model ara/), { target: { value: "sonnet" } });
    expect(within(list).queryByText("gpt-6-luna")).toBeNull();
    fireEvent.click(within(list).getByText("claude-sonnet-5"));
    fireEvent.click(screen.getByRole("radio", { name: "Özel izinler" }));
    fireEvent.click(screen.getByRole("button", { name: "Ajanı ekle" }));
    await waitFor(() => expect(calls.some(([m]) => m === "profile.upsert")).toBe(true));
    const sent = calls.find(([m]) => m === "profile.upsert")![1] as AgentProfile;
    expect(sent).toMatchObject({ name: "Ayşe", characterId: "frontend", promptMode: "own", systemPrompt: "Sen ödeme ekibindesin.", model: "claude-sonnet-5", provider: "Kira", features: ["read", "write"] });
    expect(sent.mark).toMatch(/^[a-z]+:[a-z]+$/);
    expect(sent.color).toMatch(/^#[0-9a-f]{6}$/);
  });

  it("the character's prompt as is sends no prompt of its own", async () => {
    profiles = [{ ...ayse, systemPrompt: "Tailwind kullan." }];
    render(<OfficePanel intent={{ mode: "edit", id: "p1" }} />);
    // it opens on "the character's + more", with what was added
    const extra = await screen.findByRole("radio", { name: "Karakterinki + ek" });
    expect(extra.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByRole("radio", { name: "Karakterinki" }));
    fireEvent.click(screen.getByRole("button", { name: "Kaydet" }));
    await waitFor(() => expect(calls.some(([m]) => m === "profile.upsert")).toBe(true));
    const sent = calls.find(([m]) => m === "profile.upsert")![1] as AgentProfile;
    expect(sent).toMatchObject({ id: "p1", systemPrompt: "", promptMode: "" });
    expect(sent.features ?? null).toBeNull(); // the defaults
  });
});

describe("Office side list", () => {
  beforeEach(() => { calls.length = 0; localStorage.setItem("rove.office.v2", "1"); });
  afterEach(cleanup);

  it("lists only the office's agents, with their logos; adding one goes to Settings", async () => {
    profiles = [ayse];
    const office = vi.fn();
    render(
      <SessionSidebar space="office" workspace={null} onSelectWorkspace={vi.fn()} activeSessionId="s1" onSelectSession={vi.fn()} draft={false} draftAgent={null}
        onNewChat={vi.fn()} rpcOk dark onToggleTheme={vi.fn()} onOpenMarket={vi.fn()} onOpenSettings={vi.fn()} onOfficeAgent={office} />,
    );
    const row = await screen.findByTitle("Ayşe");
    expect(row.querySelector(".agent-mark svg")).toBeTruthy();
    await waitFor(() => expect(row.textContent).toContain("Ödeme hatası"));
    // the active agent's card at the top
    expect(document.querySelector(".office-hero")?.textContent).toContain("gpt-6-luna");
    // no catalog picker in the side list any more
    expect(document.querySelector(".agent-picker")).toBeNull();
    fireEvent.click(screen.getByTitle("Ajan ekle"));
    expect(office).toHaveBeenCalledWith({ mode: "new" });
    fireEvent.click(screen.getByTitle("Ajanı düzenle"));
    expect(office).toHaveBeenCalledWith({ mode: "edit", id: "p1" });
    // the team's board is one click away
    const board = vi.fn();
    cleanup();
    render(
      <SessionSidebar space="office" workspace={null} onSelectWorkspace={vi.fn()} activeSessionId={null} onSelectSession={vi.fn()} draft={false} draftAgent={null}
        onNewChat={vi.fn()} rpcOk dark onToggleTheme={vi.fn()} onOpenMarket={vi.fn()} onOpenSettings={vi.fn()} onOfficeAgent={office} onBoard={board} />,
    );
    const boardRow = await waitFor(() => {
      const r = document.querySelector(".staff-board-row") as HTMLElement | null;
      expect(r).toBeTruthy();
      return r!;
    });
    expect(boardRow.className).toContain("active");
    fireEvent.click(boardRow);
    expect(board).toHaveBeenCalled();
  });

  // Code: a new session asks which mode it is for
  it("a new Code session is opened in the mode picked from the + menu", async () => {
    profiles = [];
    const newChat = vi.fn();
    render(
      <SessionSidebar space="chat" workspace={null} onSelectWorkspace={vi.fn()} activeSessionId="s1" onSelectSession={vi.fn()} draft={false} draftAgent={null}
        onNewChat={newChat} rpcOk dark onToggleTheme={vi.fn()} onOpenMarket={vi.fn()} onOpenSettings={vi.fn()} chatMode="orchestra" onChatMode={vi.fn()} />,
    );
    fireEvent.click(screen.getByTitle("Yeni oturum"));
    const menu = await screen.findByRole("menu", { name: "Yeni oturum hangi modda?" });
    expect(Array.from(menu.querySelectorAll("[role=menuitem] strong")).map((x) => x.textContent)).toEqual(["Orkestra", "Teamwork", "Terminal"]);
    fireEvent.click(Array.from(menu.querySelectorAll("[role=menuitem]")).find((b) => b.textContent?.includes("Teamwork"))!);
    expect(newChat).toHaveBeenCalledWith(null, "teamwork");
    expect(screen.queryByRole("menu", { name: "Yeni oturum hangi modda?" })).toBeNull();
  });
});
