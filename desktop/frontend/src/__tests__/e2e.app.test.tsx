// End-to-end: the whole <App/> in jsdom against a real daemon built from this
// repo, started on a throwaway data dir and port. It walks the flows a user
// clicks through: first chat, drafts, profiles, teams and member channels,
// opening a folder. Run with `npm run test:e2e` (needs Go); plain `npm test`
// skips it.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
const sh = (cwd: string, ...args: string[]) => {
  const r = spawnSync(args[0], args.slice(1), { cwd, encoding: "utf8" });
  if (r.status !== 0) throw new Error(`${args.join(" ")}: ${r.stderr}`);
  return r.stdout;
};

const E2E = Boolean(process.env.ROVE_E2E);
const REPO = resolve(__dirname, "../../../..");
const PORT = 7600 + Math.floor(Math.random() * 300);
const BASE = `http://127.0.0.1:${PORT}`;
const T = 20_000;

let daemon: ChildProcess | null = null;
let home = "";

// jsdom has no EventSource; the app's live updates ride on it. A minimal
// fetch-stream version is enough for the daemon's SSE frames.
class FetchEventSource {
  onmessage: ((m: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  // jsdom's AbortController is not the one Node's fetch accepts, so closing
  // cancels the body reader instead of aborting the request
  private reader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  private closed = false;
  constructor(url: string) {
    void this.run(url);
  }
  private async run(url: string) {
    try {
      const res = await fetch(url);
      this.reader = res.body!.getReader();
      if (this.closed) return void this.reader.cancel();
      const dec = new TextDecoder();
      let buf = "";
      for (;;) {
        const { value, done } = await this.reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let i: number;
        while ((i = buf.indexOf("\n\n")) >= 0) {
          const frame = buf.slice(0, i);
          buf = buf.slice(i + 2);
          const data = frame.split("\n").filter((l) => l.startsWith("data:")).map((l) => l.slice(5).trimStart()).join("\n");
          if (data) this.onmessage?.({ data });
        }
      }
    } catch (err) {
      if (!this.closed) console.error("event stream:", err);
    }
  }
  close() {
    this.closed = true;
    void this.reader?.cancel().catch(() => {});
  }
}

async function rpc<T>(method: string, params: unknown = {}): Promise<T> {
  const token = readFileSync(join(home, "auth.token"), "utf8");
  const r = await fetch(`${BASE}/rpc`, { method: "POST", body: JSON.stringify({ method, params, token }) });
  const body = (await r.json()) as { ok: boolean; result: T; error?: string };
  if (!body.ok) throw new Error(body.error);
  return body.result;
}
// the daemon encodes an empty list as null
const list = async <T,>(method: string, params: unknown = {}) => (await rpc<T[] | null>(method, params)) ?? [];

const sidebar = () => document.querySelector(".shell-side") as HTMLElement;
// Orchestra's team: a row of chips over the message box
const teamBar = () => document.querySelector(".persona-bar .team") as HTMLElement | null;
const rows = () => Array.from(sidebar().querySelectorAll(".sess-row"));
const composer = () => screen.getByPlaceholderText("Mesaj…") as HTMLTextAreaElement;
async function send(text: string) {
  await waitFor(() => expect(composer().disabled).toBe(false), { timeout: T });
  fireEvent.change(composer(), { target: { value: text } });
  fireEvent.submit(composer().closest("form")!);
}
const transcript = () => document.querySelector(".thread-viewport")?.textContent ?? "";
const replies = () => document.querySelectorAll(".thread-viewport .turn-assistant").length;

describe.skipIf(!E2E)("Rove app end to end", () => {
  beforeAll(async () => {
    home = mkdtempSync(join(tmpdir(), "rove-e2e-"));
    const bin = join(home, "rovecode");
    const build = spawnSync("go", ["build", "-o", bin, "./cmd/sextant"], { cwd: REPO, encoding: "utf8" });
    if (build.status !== 0) throw new Error(build.stderr);
    writeFileSync(join(home, "config.json"), JSON.stringify({ dataDir: home, listenHttp: `127.0.0.1:${PORT}`, listenIpc: join(home, "d.sock") }));
    // cwd is the throwaway dir: a chat without a folder works there, never in
    // this repo (/checkpoint and /goal run git in the workspace)
    daemon = spawn(bin, ["daemon"], { cwd: home, env: { ...process.env, ROVECODE_HOME: home }, stdio: "ignore" });
    for (let i = 0; i < 100; i++) {
      try {
        if ((await fetch(`${BASE}/health`)).ok) break;
      } catch { /* not yet */ }
      await new Promise((r) => setTimeout(r, 100));
    }
    localStorage.setItem("aether.http", BASE);
    localStorage.setItem("aether.token", readFileSync(join(home, "auth.token"), "utf8"));
    localStorage.setItem("aether.lang", "tr");
    Object.assign(window, { EventSource: FetchEventSource });
    Element.prototype.scrollIntoView = () => {};
    window.innerWidth = 1440;
  }, 120_000);

  afterAll(() => {
    cleanup(); // unmount first: closes the app's event stream before the daemon goes
    daemon?.kill("SIGTERM");
    rmSync(home, { recursive: true, force: true });
  });

  it("walks every stage", async () => {
    const { App } = await import("../app/App");
    render(<App />);
    const space = (name: string) => fireEvent.click(within(document.querySelector(".tb-modes") as HTMLElement).getByRole("tab", { name }));
    const personaChip = () => document.querySelector(".persona-bar")?.textContent ?? "";
    const agentRow = (name: string) =>
      Array.from(sidebar().querySelectorAll(".agent-chat-row")).find((r) => r.querySelector(".agent-row strong")?.textContent === name) as HTMLElement | undefined;
    const rowTitled = (title: string) => rows().find((r) => r.textContent?.includes(title)) as HTMLElement | undefined;
    const pickMode = async (name: RegExp) => {
      fireEvent.click(within(sidebar()).getByTitle(/Modlar/));
      fireEvent.click(await within(document.querySelector(".modes-menu") as HTMLElement).findByRole("menuitemradio", { name }));
    };

    // 1. three spaces; a first launch opens Office, waiting for an agent
    await waitFor(() => expect(sidebar()?.textContent).toContain("bağlı"), { timeout: T });
    const tabs = within(document.querySelector(".tb-modes") as HTMLElement).getAllByRole("tab").map((b) => b.textContent);
    expect(tabs).toEqual(["Agent", "Code", "Otomasyon"]);
    expect(sidebar().querySelector(".side-title")?.textContent).toBe("Ajanlar");
    // the Agent space opens on its team's board, empty for now
    await waitFor(() => expect(document.querySelector(".staff-empty")?.textContent).toContain("Ekibin boş"), { timeout: T });
    expect(document.querySelector(".shell-right")).toBeNull(); // no right panel at all
    expect(teamBar()).toBeNull(); // and no team in Office
    expect(await list("session.list")).toHaveLength(0);

    // 2. Office: agents are made in Settings → Office; only they are listed,
    //    and a row is the chat with that agent
    expect(sidebar().textContent).toContain("Ekibinde henüz ajan yok");
    expect(sidebar().querySelector(".agent-picker")).toBeNull();
    fireEvent.click(within(sidebar()).getByTitle("Ajan ekle"));
    const nameBox = await screen.findByLabelText("Ad", {}, { timeout: T });
    fireEvent.change(nameBox, { target: { value: "Reklam & Pazarlama Uzmanı" } });
    // the catalog loads once, async
    fireEvent.click((await screen.findByText("Reklam & Pazarlama Uzmanı", { selector: ".office-form .persona-card strong" }, { timeout: T })).closest(".persona-card")!);
    fireEvent.click(screen.getByRole("button", { name: "Ajanı ekle" }));
    await waitFor(() => expect(document.querySelector(".office-row")?.textContent).toContain("Reklam & Pazarlama Uzmanı"), { timeout: T });
    fireEvent.click(screen.getByTitle("Kapat"));
    await waitFor(() => expect(agentRow("Reklam & Pazarlama Uzmanı")).toBeTruthy(), { timeout: T });
    // the agent's logo: the marketing field's shape
    expect(agentRow("Reklam & Pazarlama Uzmanı")!.querySelector(".agent-mark svg")).toBeTruthy();
    fireEvent.click(agentRow("Reklam & Pazarlama Uzmanı")!.querySelector(".agent-row")!);
    await send("Yeni ürün için reklam metni yaz");
    await waitFor(() => expect(agentRow("Reklam & Pazarlama Uzmanı")?.textContent).toContain("Yeni ürün için reklam metni yaz"), { timeout: T });
    await waitFor(() => expect(replies()).toBeGreaterThan(0), { timeout: T });
    await waitFor(() => expect(personaChip()).toContain("Reklam & Pazarlama Uzmanı"), { timeout: T });
    expect(personaChip()).not.toMatch(/özellik|token|≈/);
    const adsChat = (await list<{ id: string; title: string; space: string }>("session.list")).find((x) => x.title === "Yeni ürün için reklam metni yaz")!;
    expect(adsChat.space).toBe("office");
    expect(teamBar()).toBeNull();

    // 3. a second agent, then back to the first by its row; /new keeps the agent
    await rpc("profile.upsert", { name: "Frontend Uzmanı", characterId: "frontend" });
    window.dispatchEvent(new Event("rove:profiles"));
    await waitFor(() => expect(agentRow("Frontend Uzmanı")).toBeTruthy(), { timeout: T });
    fireEvent.click(agentRow("Frontend Uzmanı")!.querySelector(".agent-row")!);
    await send("Butonları yuvarlak yap");
    await waitFor(() => expect(agentRow("Frontend Uzmanı")?.textContent).toContain("Butonları yuvarlak yap"), { timeout: T });
    fireEvent.click(agentRow("Reklam & Pazarlama Uzmanı")!.querySelector(".agent-row")!);
    await waitFor(() => expect(transcript()).toContain("Yeni ürün için reklam metni yaz"), { timeout: T });
    expect(transcript()).not.toContain("Butonları yuvarlak yap");
    await send("/new");
    await waitFor(() => expect(replies()).toBe(0), { timeout: T });
    await send("Instagram kampanyası planla");
    await waitFor(() => expect(agentRow("Reklam & Pazarlama Uzmanı")?.textContent).toContain("Instagram kampanyası planla"), { timeout: T });
    expect(sidebar().querySelectorAll(".agent-chat-row")).toHaveLength(2);

    // 4. Chat: only sessions, none of Office's; a draft opens by itself
    space("Code");
    await waitFor(() => expect(sidebar().querySelector(".side-title")?.textContent).toBe("Oturumlar"), { timeout: T });
    expect(rows()).toHaveLength(0);
    await waitFor(() => expect(composer().disabled).toBe(false), { timeout: T });
    // a first chat opens in Orchestra: no cast over the message box
    expect(teamBar()).toBeNull();
    await send("Giriş sayfasına Google ile oturum açma ekle. Sonra testleri çalıştır");
    await waitFor(() => expect(rows()).toHaveLength(1), { timeout: T });
    await waitFor(() => expect(rows()[0].textContent).toContain("Giriş sayfasına Google ile oturum açma ekle"), { timeout: T });
    await waitFor(() => expect(replies()).toBeGreaterThan(0), { timeout: T });
    expect(rows()[0].querySelector(".avatar")).toBeNull();
    expect(personaChip()).toContain("Genel asistan");
    // titles change live (session.updated events), not on the 12 s poll
    const loginChat = (await list<{ id: string; title: string; space: string }>("session.list")).find((x) => x.title.startsWith("Giriş sayfasına"))!;
    expect(loginChat.space).toBe("chat");
    await rpc("session.rename", { id: loginChat.id, title: "Canlı başlık" });
    await waitFor(() => expect(rows()[0].textContent).toContain("Canlı başlık"), { timeout: 3000 });

    // 5. "+" asks the mode, then opens a clean draft; nothing is created
    //    until a message is sent
    fireEvent.click(within(sidebar()).getByTitle("Yeni oturum"));
    fireEvent.click(within(await screen.findByRole("menu", { name: "Yeni oturum hangi modda?" })).getByText("Orkestra").closest("button")!);
    await waitFor(() => expect(replies()).toBe(0), { timeout: T });
    expect(screen.queryByRole("button", { name: /Durdur/ })).toBeNull();
    expect(rows()).toHaveLength(1);
    await send("Ürün sayfası tasarla");
    await waitFor(() => expect(rows()).toHaveLength(2), { timeout: T });

    // 6. modes (Orchestra, Teamwork, Terminal — Chat is the space, not a mode)
    fireEvent.click(within(sidebar()).getByTitle(/Modlar/));
    const menuItems = within(document.querySelector(".modes-menu") as HTMLElement).getAllByRole("menuitemradio").map((b) => b.querySelector("strong")?.textContent);
    expect(menuItems).toEqual(["Orkestra", "Teamwork", "Terminal"]);
    fireEvent.click(within(sidebar()).getByTitle(/Modlar/));
    // the login chat on Terminal
    fireEvent.click(rowTitled("Canlı başlık")!.querySelector(".sess-main")!);
    await pickMode(/Terminal/);
    await waitFor(() => expect(document.querySelector(".deck")).toBeTruthy(), { timeout: T });
    expect(teamBar()).toBeNull(); // a terminal has no team bar
    // a chat opened in a mode picked from the "+" menu keeps that mode
    fireEvent.click(rowTitled("Ürün sayfası tasarla")!.querySelector(".sess-main")!);
    await waitFor(() => expect(sidebar().querySelector(".sess-row.active")?.textContent).toContain("Ürün sayfası tasarla"), { timeout: T });
    await waitFor(() => expect(transcript()).toContain("Ürün sayfası tasarla"), { timeout: T });
    expect(document.querySelector(".deck")).toBeNull();
    fireEvent.click(rowTitled("Canlı başlık")!.querySelector(".sess-main")!);
    await waitFor(() => expect(document.querySelector(".deck")).toBeTruthy(), { timeout: T });
    fireEvent.click(rowTitled("Ürün sayfası tasarla")!.querySelector(".sess-main")!);
    await waitFor(() => expect(transcript()).toContain("Ürün sayfası tasarla"), { timeout: T });
    expect(document.querySelector(".deck")).toBeNull();

    // 7. Orchestra has no cast: no characters to add, the chat splits its
    // work between plain subagents on its own
    const productChat = (await list<{ id: string; title: string }>("session.list")).find((x) => x.title === "Ürün sayfası tasarla")!;
    expect(teamBar()).toBeNull();

    // 8. Terminal and Teamwork, then back to Orchestra
    await pickMode(/Terminal/);
    await waitFor(() => expect(document.querySelector(".deck")).toBeTruthy(), { timeout: T });
    // Teamwork: give a job, see the plan as cards, nothing runs until approved
    await pickMode(/Teamwork/);
    const ask = await screen.findByPlaceholderText(/Google ile oturum/);
    fireEvent.change(ask, { target: { value: "README dosyası yaz" } });
    fireEvent.click(screen.getByRole("button", { name: "Plan çıkar" }));
    await waitFor(() => expect(document.querySelectorAll(".tw-card")).toHaveLength(1), { timeout: T });
    // this daemon's model answers in prose: the job becomes one phase, and the note says why
    expect(document.querySelector(".tw-thread")?.textContent).toMatch(/Planlayıcı geçerli bir plan veremedi/);
    expect(document.querySelector(".tw-status")?.textContent).toBe("Onay bekliyor");
    const drafted = await rpc<{ id: string; status: string; phases: { agents: { channelId?: string }[] }[] }>("teamwork.get", { sessionId: productChat.id });
    expect(drafted.status).toBe("draft");
    expect(drafted.phases[0].agents[0].channelId ?? "").toBe(""); // no channel, no run, before approval
    fireEvent.click(screen.getByRole("button", { name: "Onayla ve başlat" }));
    await waitFor(() => expect(document.querySelector(".tw-status")?.textContent).toBe("Bitti"), { timeout: T });
    expect(document.querySelector(".tw-result")).toBeTruthy();
    expect((await rpc<{ status: string }>("teamwork.get", { sessionId: productChat.id })).status).toBe("done");
    // the phase's channel is not a chat and not a team member
    expect(rows()).toHaveLength(2);
    await pickMode(/Orkestra/);
    await waitFor(() => expect(transcript()).toContain("Ürün sayfası tasarla"), { timeout: T });
    expect(JSON.parse(localStorage.getItem("aether.chat.modes") || "{}")).toEqual({ [loginChat.id]: "terminal", [productChat.id]: "orchestra", last: "orchestra" });

    // 9. open a folder (a git repo), then chat in it
    const folder = join(home, "benim-projem");
    mkdirSync(folder);
    sh(folder, "git", "init", "-q");
    sh(folder, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init");
    writeFileSync(join(folder, "app.txt"), "v1\n");
    sh(folder, "git", "add", "app.txt");
    sh(folder, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "app");
    window.prompt = () => folder;
    fireEvent.click(within(sidebar()).getByTitle("Klasör aç"));
    fireEvent.click(await within(sidebar()).findByText(/Klasör aç…/));
    await waitFor(() => expect(replies()).toBe(0), { timeout: T });
    await send("README dosyası ekle");
    await waitFor(() => expect(rows()).toHaveLength(3), { timeout: T });
    await waitFor(() => expect(rows()[0].textContent).toContain(basename(folder)), { timeout: T });
    const sessions = await list<{ id: string; title: string; workspaceId: string; agentId: string }>("session.list");
    const ws = await list<{ id: string; path: string }>("workspace.list");
    const folderWs = ws.find((w) => w.id === sessions[0].workspaceId)!;
    expect(folderWs.path).toMatch(/benim-projem$/);

    // 10. commands, each checked against the daemon
    await send("/rename README işi");
    await waitFor(() => expect(rows()[0].textContent).toContain("README işi"), { timeout: T });
    await send("/remember Testler vitest ile koşar");
    await waitFor(async () => expect((await list<{ content: string }>("memory.list", { scope: "workspace", scopeId: folderWs.id })).map((m) => m.content)).toContain("Testler vitest ile koşar"), { timeout: T });
    await send("/goal README yaz | kurulum adımları var");
    await waitFor(async () => expect((await list<{ title: string; completionContract: { criteria: string[] } }>("goal.list")).find((g) => g.title === "README yaz")?.completionContract.criteria).toEqual(["kurulum adımları var"]), { timeout: T });
    // the goal works in this chat with its agent (it used to be created
    // without one and never ran), and /stop stops it
    type GoalRow = { id: string; title: string; agentId: string; sessionId?: string; status: string; iteration: number };
    const readmeGoal = (await list<GoalRow>("goal.list")).find((g) => g.title === "README yaz")!;
    expect(readmeGoal.sessionId).toBe(sessions[0].id);
    expect(readmeGoal.agentId).toBe(sessions[0].agentId);
    await waitFor(async () => expect((await rpc<GoalRow>("goal.get", { id: readmeGoal.id })).iteration).toBeGreaterThan(0), { timeout: T });
    await send("/stop");
    await waitFor(async () => expect(["canceled", "blocked", "done"]).toContain((await rpc<GoalRow>("goal.get", { id: readmeGoal.id })).status), { timeout: T });
    writeFileSync(join(folder, "app.txt"), "v2\n");
    await send("/checkpoint önce");
    await waitFor(() => expect(sh(folder, "git", "stash", "list")).toContain("önce"), { timeout: T });
    expect(readFileSync(join(folder, "app.txt"), "utf8")).toBe("v2\n");
    writeFileSync(join(folder, "app.txt"), "v3 broken\n");
    await send("/restore");
    await waitFor(() => expect(readFileSync(join(folder, "app.txt"), "utf8")).toBe("v2\n"), { timeout: T });
    // a command missing its argument says how it is used
    await send("/rename");
    await waitFor(() => expect(document.querySelector(".composer-dock .status-stack")?.textContent).toContain("/rename"), { timeout: T });

    // /models: a picker of every provider's models; the pick is this chat's
    await rpc("provider.upsert", { name: "yerel", kind: "fake", models: ["m-kucuk", "m-buyuk"] });
    await send("/models");
    await waitFor(() => expect(document.querySelector(".slash-menu[aria-label]")?.textContent).toContain("m-buyuk"), { timeout: T });
    fireEvent.change(composer(), { target: { value: "buyuk" } });
    fireEvent.keyDown(composer(), { key: "Enter" });
    await waitFor(async () => expect(await rpc<{ model: string; source: string }>("session.model", { sessionId: sessions[0].id })).toMatchObject({ model: "m-buyuk", source: "chat" }), { timeout: T });
    expect(document.querySelector(".slash-menu")).toBeNull();
    await send("/models varsayılan-yok-böyle-model");
    await waitFor(() => expect(document.querySelector(".slash-menu[aria-label]")).toBeTruthy(), { timeout: T });
    fireEvent.keyDown(composer(), { key: "Escape" });
    expect(document.querySelector(".slash-menu")).toBeNull();

    // 10b. Terminal: one session split into terminals that work as a team.
    // A new chat not written to yet is a terminal you can type in.
    const beforeTerm = rows().length;
    // a new session in Terminal straight from the "+" menu
    fireEvent.click(within(sidebar()).getByTitle("Yeni oturum"));
    fireEvent.click(within(await screen.findByRole("menu", { name: "Yeni oturum hangi modda?" })).getByText("Terminal").closest("button")!);
    const paneBox = (label: string) => document.querySelector(`.deck > .pane[data-pane="${label}"] .term-textarea`) as HTMLTextAreaElement | null;
    const t1Box = await waitFor(() => {
      const ta = document.querySelector(".deck .pane .term-textarea") as HTMLTextAreaElement | null;
      expect(ta?.disabled).toBe(false);
      return ta!;
    }, { timeout: T });
    expect(rows()).toHaveLength(beforeTerm); // nothing created yet
    fireEvent.click(within(document.querySelector(".deck-bar") as HTMLElement).getByRole("radio", { name: /^2/ }));
    await waitFor(() => expect(document.querySelectorAll(".deck > .pane")).toHaveLength(2), { timeout: T });
    fireEvent.change(t1Box, { target: { value: "Terminalden merhaba" } });
    fireEvent.submit(t1Box.closest("form")!);
    await waitFor(() => expect(rows()).toHaveLength(beforeTerm + 1), { timeout: T });
    await waitFor(() => expect(document.querySelector(".deck-title")?.textContent).toContain("Terminalden merhaba"), { timeout: T });
    // the second terminal's first message opens it under the same session:
    // one row in the list, two terminals in the session
    const t2Box = document.querySelectorAll(".deck > .pane .term-textarea")[1] as HTMLTextAreaElement;
    fireEvent.change(t2Box, { target: { value: "İkinci terminal" } });
    fireEvent.submit(t2Box.closest("form")!);
    await waitFor(() => expect(paneBox("T2")).toBeTruthy(), { timeout: T });
    expect(rows()).toHaveLength(beforeTerm + 1);
    const termSession = (await list<{ id: string; title: string }>("session.list")).find((x) => x.title.includes("Terminalden merhaba"))!;
    const tPanes = await rpc<{ label: string; id: string }[]>("terminal.panes", { sessionId: termSession.id });
    expect(tPanes.map((p) => p.label)).toEqual(["T1", "T2"]);
    // terminals carry no characters: /character is no command, panes are
    // named by their sessions
    expect(document.querySelector('.deck > .pane[data-pane="T2"] .pane-title')?.textContent).not.toMatch(/Uzman/);
    expect(document.querySelector(".term-char, .term-mode-agent")).toBeNull();
    // /panes in T1 lists both; @T2 hands T2 work
    fireEvent.change(paneBox("T1")!, { target: { value: "/panes" } });
    fireEvent.submit(paneBox("T1")!.closest("form")!);
    await waitFor(() => expect(document.querySelector('.deck > .pane[data-pane="T1"] .term-note')?.textContent).toContain("T2"), { timeout: T });
    fireEvent.change(paneBox("T1")!, { target: { value: "@T2 testleri yaz" } });
    fireEvent.submit(paneBox("T1")!.closest("form")!);
    await waitFor(async () => {
      const h = await rpc<{ role: string; content: string }[]>("session.history", { sessionId: tPanes[1].id });
      expect(h.some((m) => m.role === "user" && m.content === "testleri yaz")).toBe(true);
    }, { timeout: T });
    // /session switches the whole deck to another session
    fireEvent.change(paneBox("T1")!, { target: { value: "/session README" } });
    fireEvent.submit(paneBox("T1")!.closest("form")!);
    await waitFor(() => expect(sidebar().querySelector(".sess-row.active")?.textContent).toContain("README işi"), { timeout: T });
    await waitFor(() => expect(document.querySelector(".deck-title")?.textContent).toContain("README işi"), { timeout: T });
    fireEvent.click(within(document.querySelector(".deck-bar") as HTMLElement).getByRole("radio", { name: /^1/ }));

    // 11. Automation: the map on the left, one sidebar on the right whose top
    // picks the map and whose conversations (Agents or Sessions) to list
    space("Otomasyon");
    await waitFor(() => expect(document.querySelector(".auto-picks")).toBeTruthy(), { timeout: T });
    expect(sidebar()).toBeNull(); // a full page
    const picks = () => document.querySelector(".auto-picks") as HTMLElement;
    expect(picks().parentElement?.className).toMatch(/map-side|cmap-side/); // at the top of the map's sidebar
    expect(within(picks()).getAllByRole("tab").map((b) => b.textContent?.trim())).toEqual(["Bağlam", "Oturum", "Kod"]);
    expect(within(picks()).getAllByRole("radio").map((b) => b.textContent?.trim())).toEqual(["Ajanlar", "Oturumlar"]);
    // Context Map: a chat's own context, read from its history — no folder
    fireEvent.click(within(picks()).getByRole("tab", { name: /Bağlam Haritası/ }));
    await waitFor(() => expect(document.querySelector(".cx-sub")?.textContent).toMatch(/\d+ istek · \d+ araç çağrısı/), { timeout: T });
    expect(document.querySelector(".cx-window")?.textContent).toContain("Modelin gördüğü sohbet");
    // a graph first; the same context as a list one click away
    expect(document.querySelector(".cx-graph canvas")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: "Liste" }));
    await waitFor(() => expect(document.querySelectorAll(".cx-turn").length).toBeGreaterThan(0), { timeout: T });
    fireEvent.click(screen.getByRole("radio", { name: "Ağ" }));
    // the assistant beside the map: a hidden child of the chat that talks
    // about its context
    fireEvent.click(screen.getByRole("button", { name: /Bağlama sor/ }));
    const ca = await waitFor(() => { const el = document.querySelector(".ca") as HTMLElement | null; expect(el).toBeTruthy(); return el!; }, { timeout: T });
    fireEvent.click(await within(ca).findByRole("button", { name: "Bağlamı en çok ne dolduruyor?" }, { timeout: T }));
    await waitFor(() => expect(ca.querySelector(".ca-msg.assistant .ca-reply")).toBeTruthy(), { timeout: T });
    expect(ca.querySelector(".ca-msg.user")?.textContent).toBe("Bağlamı en çok ne dolduruyor?");
    // tucked away it is hidden, not gone: the talk is still there on return
    fireEvent.click(within(ca).getByRole("button", { name: /^Gizle/ }));
    expect(ca.className).toContain("is-hidden");
    fireEvent.click(screen.getByRole("button", { name: /Bağlama sor/ }));
    expect(ca.className).not.toContain("is-hidden");
    expect(ca.querySelector(".ca-msg.user")?.textContent).toBe("Bağlamı en çok ne dolduruyor?");
    fireEvent.click(within(ca).getByRole("button", { name: /^Gizle/ }));
    fireEvent.click(within(picks()).getByRole("tab", { name: /Oturum Haritası/ }));
    const mapItems = () => Array.from(document.querySelectorAll(".cmap-side .cmap-item")).map((x) => x.textContent ?? "");
    fireEvent.click(within(picks()).getByRole("radio", { name: "Ajanlar" }));
    await waitFor(() => expect(mapItems().join("|")).toContain("Instagram kampanyası planla"), { timeout: T });
    expect(mapItems().join("|")).not.toContain("README işi");
    fireEvent.click(within(picks()).getByRole("radio", { name: "Oturumlar" }));
    await waitFor(() => expect(mapItems().join("|")).toContain("README işi"), { timeout: T });
    expect(mapItems().join("|")).not.toContain("Instagram kampanyası planla");
    // Context Map: the listed conversations are where map files get shared
    fireEvent.click(within(picks()).getByRole("tab", { name: /Kod Haritası/ }));
    await waitFor(() => expect(document.querySelector(".map-side .auto-picks")).toBeTruthy(), { timeout: T });
    const conv = await waitFor(() => {
      const b = Array.from(document.querySelectorAll(".map-side .map-conv")).find((x) => x.textContent?.includes("README işi")) as HTMLElement | undefined;
      expect(b).toBeTruthy();
      return b!;
    }, { timeout: T });
    // the Chat space's open chat is the default target; clicking toggles it
    await waitFor(() => expect(conv.getAttribute("aria-pressed")).toBe("true"), { timeout: T });
    fireEvent.click(conv);
    await waitFor(() => expect(conv.getAttribute("aria-pressed")).toBe("false"), { timeout: T });
    fireEvent.click(conv);
    await waitFor(() => expect(conv.getAttribute("aria-pressed")).toBe("true"), { timeout: T });

    // 12. Office again: an agent is deleted in Settings → Office, in two
    //     clicks, and its chats go with it
    space("Agent");
    // wait for the row's chats, not just the row (chats load after agents)
    await waitFor(() => expect(agentRow("Reklam & Pazarlama Uzmanı")?.textContent).toContain("Instagram kampanyası planla"), { timeout: T });
    fireEvent.click(within(agentRow("Reklam & Pazarlama Uzmanı")!).getByTitle("Ajanı düzenle"));
    fireEvent.click(await screen.findByRole("button", { name: "← Agent" }, { timeout: T }));
    const adsRow = await waitFor(() => {
      const r = Array.from(document.querySelectorAll(".office-row")).find((x) => x.textContent?.includes("Reklam & Pazarlama Uzmanı")) as HTMLElement | undefined;
      expect(r?.textContent).toContain("2 sohbet");
      return r!;
    }, { timeout: T });
    fireEvent.click(within(adsRow).getByTitle("Ajan ve 2 sohbeti silinir"));
    expect(within(adsRow).getByText("2 sohbetle sil?")).toBeTruthy();
    fireEvent.click(within(adsRow).getByText("2 sohbetle sil?"));
    await waitFor(() => expect(Array.from(document.querySelectorAll(".office-row")).some((x) => x.textContent?.includes("Reklam"))).toBe(false), { timeout: T });
    fireEvent.click(screen.getByTitle("Kapat"));
    await waitFor(() => expect(agentRow("Reklam & Pazarlama Uzmanı")).toBeUndefined(), { timeout: T });
    const left = (await list<{ title: string }>("session.list")).map((x) => x.title);
    expect(left).not.toContain("Instagram kampanyası planla");
    expect(left).toContain("Butonları yuvarlak yap");
    expect(left).toContain("README işi");

    // 13a. Settings → Providers: adding one fetches its models from the base
    // URL (an OpenAI-compatible /v1/models), and ↻ picks up a changed list
    let served = ["qwen3-max", "qwen3-coder"];
    let sawKey = "";
    const models = createServer((req, res) => {
      sawKey = String(req.headers.authorization ?? "");
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ object: "list", data: served.map((id) => ({ id, object: "model" })) }));
    });
    await new Promise<void>((r) => models.listen(0, "127.0.0.1", () => r()));
    const modelsURL = `http://127.0.0.1:${(models.address() as { port: number }).port}/v1`;
    fireEvent.click(within(sidebar()).getByTitle("Ayarlar"));
    fireEvent.click(await screen.findByRole("button", { name: /Sağlayıcılar/ }));
    fireEvent.change(await screen.findByPlaceholderText("my-provider"), { target: { value: "qwen" } });
    fireEvent.change(screen.getByPlaceholderText("https://api.example.com/v1"), { target: { value: modelsURL } });
    fireEvent.change(screen.getByPlaceholderText("sk-…"), { target: { value: "sk-e2e" } });
    fireEvent.click(screen.getByRole("button", { name: "Kaydet" }));
    const provRow = () => document.querySelector('.prov-row[data-provider="qwen"]') as HTMLElement | null;
    await waitFor(() => expect(Array.from(provRow()?.querySelectorAll(".prov-models code") ?? []).map((c) => c.textContent)).toEqual(["qwen3-coder", "qwen3-max"]), { timeout: T });
    expect(sawKey).toBe("Bearer sk-e2e"); // the key was stored before the models were asked for
    served = ["qwen3.8-max", "qwen3-max"];
    fireEvent.click(within(provRow()!).getByRole("button", { name: /Modelleri yenile/ }));
    await waitFor(() => expect(provRow()?.textContent).toContain("qwen3.8-max"), { timeout: T });
    expect(provRow()?.textContent).not.toContain("qwen3-coder");
    // the fetched models are what the chat's model picker offers
    expect((await list<{ provider: string; model: string }>("model.list")).filter((m) => m.provider === "qwen").map((m) => m.model)).toEqual(["qwen3-max", "qwen3.8-max"]);
    models.close();

    // 13. Settings → Office: an agent from scratch, with a prompt of its own
    //     and one of the models just added; deleted again in two clicks
    fireEvent.click(await screen.findByRole("button", { name: /^Agent$/ }));
    fireEvent.click(await screen.findByRole("button", { name: /Yeni ajan/ }));
    fireEvent.change(await screen.findByLabelText("Ad"), { target: { value: "Test Ajanı" } });
    fireEvent.change(screen.getByPlaceholderText(/ödeme servisinden/), { target: { value: "Yalnızca Go kodu yazarsın." } });
    fireEvent.change(screen.getByPlaceholderText(/Model ara/), { target: { value: "qwen3-max" } });
    fireEvent.click(await within(screen.getByRole("listbox", { name: "Model" })).findByText("qwen3-max", {}, { timeout: T }));
    fireEvent.click(screen.getByRole("button", { name: "Ajanı ekle" }));
    const officeRow = (name: string) => Array.from(document.querySelectorAll(".office-row")).find((r) => r.textContent?.includes(name)) as HTMLElement | undefined;
    await waitFor(() => expect(officeRow("Test Ajanı")?.textContent).toContain("qwen3-max"), { timeout: T });
    const made = (await list<{ name: string; model: string; provider: string; systemPrompt: string; mark: string }>("profile.list")).find((p) => p.name === "Test Ajanı")!;
    expect(made).toMatchObject({ model: "qwen3-max", provider: "qwen", systemPrompt: "Yalnızca Go kodu yazarsın." });
    expect(made.mark).toMatch(/^[a-z]+:prompt$/);
    fireEvent.click(within(officeRow("Test Ajanı")!).getByText("Sil"));
    expect((await list<{ name: string }>("profile.list")).map((p) => p.name)).toContain("Test Ajanı");
    fireEvent.click(within(officeRow("Test Ajanı")!).getByText("Silinsin mi?"));
    await waitFor(async () => expect((await list<{ name: string }>("profile.list")).map((p) => p.name)).not.toContain("Test Ajanı"), { timeout: T });

    // 14. in English everything reads in English: spaces and agents
    fireEvent.click(screen.getByRole("button", { name: /Görünüm/ }));
    fireEvent.change(await screen.findByRole("combobox", { name: "Dil" }), { target: { value: "en" } });
    fireEvent.click(screen.getByTitle("Close"));
    await waitFor(() => expect(within(document.querySelector(".tb-modes") as HTMLElement).getAllByRole("tab").map((b) => b.textContent)).toEqual(["Agent", "Code", "Automation"]), { timeout: T });
    // an agent keeps the name it was given; the office around it is English
    await waitFor(() => expect(agentRow("Frontend Uzmanı")).toBeTruthy(), { timeout: T });
    fireEvent.click(within(sidebar()).getByTitle("Add an agent"));
    await screen.findByText("Starting character", {}, { timeout: T });
    await waitFor(() => expect(document.querySelector(".office-form")?.textContent).toContain("UI/UX Designer"), { timeout: T });
    expect(document.querySelector(".office-form")?.textContent).not.toMatch(/Tasarımcı|Yazılım/);
  }, 180_000);
});
