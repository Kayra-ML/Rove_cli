import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Session } from "../lib/types";

// The chat's newer features against a mocked daemon: images, @files, the
// queue, thinking level, reviewing a turn's changes, /compact, rules.

const calls: [string, Record<string, unknown>][] = [];
let history: Record<string, unknown>[] = [];
let edits = { runId: "R1", files: [
  { path: "src/main.go", status: "pending", change: "modified", added: 2, removed: 1, diff: "--- a/src/main.go\n+++ b/src/main.go\n@@ -1,2 +1,3 @@\n-old\n+new\n+more\n ctx" },
  { path: "docs/NEW.md", status: "pending", change: "added", added: 1, removed: 0, diff: "--- a/docs/NEW.md\n+++ b/docs/NEW.md\n@@ -0,0 +1,1 @@\n+# yeni" },
] };
let offered: { provider: string; model: string }[] = [{ provider: "openai", model: "gpt-4o" }];
let releaseSend: (() => void)[] = [];
let holdSends = false;

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: Record<string, unknown> = {}) => {
    calls.push([method, params]);
    switch (method) {
      case "agent.list": return [{ id: "a1", name: "Rove", provider: "openai", model: "gpt-4o", status: "idle", profile: "", workspaceId: "" }];
      case "session.list": return [];
      case "session.history": return history;
      case "session.model": return { provider: "openai", model: "gpt-4o", source: "agent", effort: "" };
      case "model.list": return offered;
      case "session.setEffort": return { ok: true };
      case "workspace.rules": return { files: ["AGENTS.md", ".cursorrules"] };
      case "fs.tree": return ["src/main.go", "src/util.go", "README.md", "docs/"];
      case "fs.read": return { content: `package main // ${String(params.path)}` };
      case "edits.list": return edits;
      case "edits.revert":
      case "edits.accept": {
        const status = method === "edits.revert" ? "reverted" : "accepted";
        edits = { ...edits, files: edits.files.map((f) => (!params.path || f.path === params.path) && f.status === "pending" ? { ...f, status } : f) };
        return edits;
      }
      case "session.compact": return { compacted: true };
      case "session.send":
        if (holdSends) return new Promise<void>((r) => releaseSend.push(r));
        return {};
      default: return null;
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
  pickFolder: vi.fn(),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { Chat } from "../app/Chat";

const sess: Session = { id: "S", title: "S", agentId: "a1", workspaceId: "w", updatedAt: "", space: "chat" };
const sends = () => calls.filter(([m]) => m === "session.send").map(([, p]) => p);

async function openChat() {
  render(<Chat session={sess} workspaceId="w" workspacePath="/p" onNewDraft={() => {}} />);
  const box = (await screen.findByPlaceholderText("Mesaj…")) as HTMLTextAreaElement;
  await waitFor(() => expect(box.disabled).toBe(false));
  return box;
}

describe("chat features", () => {
  beforeEach(() => {
    calls.length = 0;
    history = [];
    offered = [{ provider: "openai", model: "gpt-4o" }];
    releaseSend = [];
    holdSends = false;
    Element.prototype.scrollIntoView = () => {};
  });
  afterEach(cleanup);

  // An agent keeps the model it was made with. If that provider is gone,
  // naming it on the button is a lie: the chat would not run on it.
  it("names the model only while a provider still offers it", async () => {
    await openChat();
    const pill = () => screen.getByRole("button", { name: "Model seç" });
    await waitFor(() => expect(pill().textContent).toContain("gpt-4o"));

    cleanup();
    offered = []; // the provider went away
    await openChat();
    await waitFor(() => expect(calls.some(([m]) => m === "model.list")).toBe(true));
    await waitFor(() => expect(pill().textContent).not.toContain("gpt-4o"));
    expect(pill().querySelector(".pill-label")?.textContent).toBe("·");
    expect(pill().textContent).not.toContain("openai"); // nor the provider

    cleanup();
    offered = [{ provider: "other", model: "llama-4" }]; // a different one
    await openChat();
    await waitFor(() => expect(pill().querySelector(".pill-label")?.textContent).toBe("·"));
  });

  // The prompt you are reading the answer to stays at the top as one line,
  // so a long transcript still says which question you are inside.
  it("pins the prompt you have scrolled past, as a single short line", async () => {
    const longPrompt = `Uzun bir istek: ${"x".repeat(400)}\nikinci satır`;
    history = [
      { id: "m1", sessionId: "S", role: "user", content: "ilk soru", createdAt: "" },
      { id: "m2", sessionId: "S", role: "assistant", content: "ilk cevap", createdAt: "" },
      { id: "m3", sessionId: "S", role: "user", content: longPrompt, createdAt: "" },
      { id: "m4", sessionId: "S", role: "assistant", content: "ikinci cevap", createdAt: "" },
    ];
    await openChat();
    const view = document.querySelector(".thread-viewport") as HTMLElement;
    const pin = () => document.querySelector(".thread-pin");
    // jsdom lays nothing out, so the turns are placed by hand
    const place = (tops: number[]) => {
      view.getBoundingClientRect = () => ({ top: 0 }) as DOMRect;
      document.querySelectorAll<HTMLElement>("[data-prompt]").forEach((el, i) => {
        el.getBoundingClientRect = () => ({ top: tops[i] }) as DOMRect;
      });
      fireEvent.scroll(view);
    };

    place([50, 300]); // both still below the top edge
    expect(pin()).toBeNull();

    place([-20, 300]); // the first has started to leave
    expect(pin()?.textContent).toBe("ilk soru");

    place([-400, -10]); // now inside the long one
    const text = pin()!.textContent!;
    expect(text.startsWith("Uzun bir istek:")).toBe(true);
    expect(text).not.toContain("ikinci satır"); // one line only
    expect(text.length).toBeLessThan(200); // and cut short
    expect(text.endsWith("…")).toBe(true);
    expect(pin()?.getAttribute("title")).toBe(text); // the rest on hover
  });

  // The box floats over the transcript as glass, so text scrolls and blurs
  // under it; once there is something in it, it turns solid to read.
  it("the message box is glass until there is something in it", async () => {
    const box = await openChat();
    const dock = () => document.querySelector(".composer-dock")!;
    expect(dock().className).not.toContain("filled");
    fireEvent.change(box, { target: { value: "merhaba" } });
    expect(dock().className).toContain("filled");
    fireEvent.change(box, { target: { value: "   " } });
    expect(dock().className).not.toContain("filled");
  });

  it("pasted images and @files go with the message, and show back as chips", async () => {
    const box = await openChat();
    const png = new File([new Uint8Array([137, 80, 78, 71])], "shot.png", { type: "image/png" });
    fireEvent.paste(box, { clipboardData: { files: [png] } });
    await waitFor(() => expect(document.querySelectorAll(".composer-image img")).toHaveLength(1));

    // @ opens the workspace's files; Enter picks one
    fireEvent.change(box, { target: { value: "bak @mai", selectionStart: 8 } });
    const menu = await screen.findByRole("listbox", { name: "Dosyalar" });
    expect(within(menu).getAllByRole("option").map((o) => o.querySelector("span")?.textContent)).toEqual(["src/main.go"]);
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(box.value).toBe("bak @src/main.go "));

    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(sends()).toHaveLength(1));
    const sent = sends()[0];
    expect(String(sent.content)).toBe('<file path="src/main.go">\npackage main // src/main.go\n</file>\n\nbak @src/main.go');
    expect((sent.images as string[])[0]).toMatch(/^data:image\/png;base64,/);
    expect(document.querySelector(".composer-image")).toBeNull();

    // the stored message shows the file as a chip, the image, and the text only
    history = [{ id: "m1", sessionId: "S", role: "user", content: sent.content, images: sent.images, createdAt: "" }];
    cleanup();
    await openChat();
    await waitFor(() => expect(document.querySelector(".msg-file")?.textContent).toBe("@main.go"));
    expect(document.querySelectorAll(".msg-images img")).toHaveLength(1);
    expect(document.querySelector(".user-bubble")?.textContent).not.toContain("package main");
  });

  it("messages written while the agent answers wait in a queue; 'send now' goes first", async () => {
    holdSends = true;
    const box = await openChat();
    fireEvent.change(box, { target: { value: "bir" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(sends()).toHaveLength(1));
    for (const text of ["iki", "üç"]) {
      fireEvent.change(box, { target: { value: text } });
      fireEvent.keyDown(box, { key: "Enter" });
    }
    const queue = await screen.findByLabelText("Sıradaki mesajlar");
    expect(Array.from(queue.querySelectorAll(".queue-text")).map((x) => x.textContent)).toEqual(["iki", "üç"]);
    expect(sends()).toHaveLength(1); // nothing else sent yet
    // steer: "üç" now — the running reply is stopped
    fireEvent.click(within(queue.querySelectorAll(".queue-item")[1] as HTMLElement).getByRole("button", { name: "Şimdi gönder" }));
    await waitFor(() => expect(calls.some(([m]) => m === "session.cancel")).toBe(true));
    await act(async () => { releaseSend.shift()?.(); });
    await waitFor(() => expect(sends().map((p) => p.content)).toEqual(["bir", "üç"]));
    await act(async () => { releaseSend.shift()?.(); });
    await waitFor(() => expect(sends().map((p) => p.content)).toEqual(["bir", "üç", "iki"]));
    await act(async () => { releaseSend.shift()?.(); });
    await waitFor(() => expect(screen.queryByLabelText("Sıradaki mesajlar")).toBeNull());
  });

  it("thinking level is picked next to the model and shown on its button", async () => {
    await openChat();
    const pill = await screen.findByRole("button", { name: "Model seç" });
    fireEvent.click(pill);
    const row = await screen.findByRole("radiogroup", { name: "Düşünme" });
    expect(within(row).getAllByRole("radio").map((b) => b.textContent)).toEqual(["Otomatik", "Düşük", "Orta", "Yüksek"]);
    fireEvent.click(within(row).getByRole("radio", { name: "Yüksek" }));
    await waitFor(() => expect(calls.find(([m]) => m === "session.setEffort")?.[1]).toEqual({ sessionId: "S", effort: "high" }));
    await waitFor(() => expect(pill.textContent).toContain("Yüksek"));
  });

  it("a turn's changes: a bar above the box, a review with diffs, revert and accept", async () => {
    await openChat();
    const bar = await waitFor(() => {
      const b = document.querySelector(".changes-bar") as HTMLElement | null;
      expect(b).toBeTruthy();
      return b!;
    });
    expect(bar.textContent).toContain("2 dosya değişti");
    expect(bar.textContent).toContain("+3");
    fireEvent.click(within(bar).getByRole("button", { name: "İncele" }));
    const sheet = await screen.findByRole("dialog", { name: "Son turun değişiklikleri" });
    expect(sheet.querySelector(".diff-view .del")?.textContent).toBe("-old");
    expect(sheet.querySelectorAll(".diff-view .add")).toHaveLength(2);
    fireEvent.click(within(sheet).getByRole("button", { name: "Geri al" }));
    await waitFor(() => expect(calls.find(([m]) => m === "edits.revert")?.[1]).toEqual({ sessionId: "S", runId: "R1", path: "src/main.go" }));
    await waitFor(() => expect(sheet.querySelector(".review-file.reverted")).toBeTruthy());
    fireEvent.click(within(sheet).getByRole("button", { name: "Tümünü kabul et" }));
    await waitFor(() => expect(calls.find(([m]) => m === "edits.accept")?.[1]).toEqual({ sessionId: "S", runId: "R1", path: "" }));
    // nothing pending: the bar goes away
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(document.querySelector(".changes-bar")).toBeNull());
  });

  it("/compact, /diff, the rules chip, and a summary in the transcript", async () => {
    history = [
      { id: "s1", sessionId: "S", role: "system", kind: "summary", content: "ÖZET: giriş sayfası yapıldı", createdAt: "" },
      { id: "u1", sessionId: "S", role: "user", content: "devam", createdAt: "" },
    ];
    const box = await openChat();
    await waitFor(() => expect(document.querySelector(".rules-chip")?.textContent).toContain("AGENTS.md +1"));
    expect(document.querySelector(".turn-summary")?.textContent).toContain("ÖZET: giriş sayfası yapıldı");
    fireEvent.change(box, { target: { value: "/compact" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(calls.find(([m]) => m === "session.compact")?.[1]).toMatchObject({ sessionId: "S" }));
    edits = { ...edits, files: edits.files.map((f) => ({ ...f, status: "pending" })) };
    fireEvent.change(box, { target: { value: "/diff" } });
    fireEvent.submit(box.closest("form")!);
    await screen.findByRole("dialog", { name: "Son turun değişiklikleri" });
  });
});
