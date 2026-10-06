import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

// Running Rove on a server, from the app's side: the title bar switch, the
// reconnecting banner, the server folder picker, and the old-daemon warning.

let health: { apiLevel?: number } = { apiLevel: 12 };
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

type Handler = (data: unknown) => void;
const handlers = new Map<string, Handler>();
const bridge = {
  Connection: vi.fn(async () => ({ status: "local" })),
  ConnectRemote: vi.fn(async (json: string) => ({ status: "connected", host: JSON.parse(json).host, http: "http://127.0.0.1:5555", token: "t" })),
  DisconnectRemote: vi.fn(async () => {}),
  RPC: vi.fn(async (method: string, params: string) => {
    if (method === "usage.get") return JSON.stringify({ ok: true, result: health });
    if (method === "fs.dirs") {
      const p = (JSON.parse(params) as { path: string }).path || "/home/u";
      return JSON.stringify({ ok: true, result: { path: p, parent: p === "/home/u" ? "/home" : "/home/u", home: "/home/u", dirs: p === "/home/u" ? ["api", "web"] : [], project: p.endsWith("api") } });
    }
    return JSON.stringify({ ok: true, result: null });
  }),
  Token: vi.fn(async () => "t"),
  HTTPAddr: vi.fn(async () => "127.0.0.1:7420"),
};

import { ConnectionBanner, ConnectionMenu } from "../app/ConnectionMenu";
import { FolderBrowserHost } from "../app/FolderBrowser";
import { ApiBanner, REQUIRED_API } from "../app/ApiBanner";
import { forgetReads, pickFolder } from "../lib/rpc";

const reload = vi.fn();

describe("running Rove on a server", () => {
  beforeEach(() => {
    handlers.clear();
    localStorage.clear();
    localStorage.setItem("aether.sshHosts", JSON.stringify([{ host: "prod", hostName: "10.0.0.5", authMethod: "agent" }]));
    Object.assign(window, {
      go: { main: { App: bridge } },
      runtime: { EventsOn: (name: string, cb: Handler) => { handlers.set(name, cb); return () => handlers.delete(name); } },
    });
    Object.defineProperty(window, "location", { value: { ...window.location, reload }, writable: true });
    reload.mockClear();
    bridge.ConnectRemote.mockClear();
  });
  afterEach(cleanup);

  it("the title bar switch runs Rove on a saved server and shows it", async () => {
    render(<ConnectionMenu lang="tr" onAddServer={() => {}} />);
    const pill = await screen.findByRole("button", { name: /Bu bilgisayar/ });
    fireEvent.click(pill);
    const menu = screen.getByRole("menu");
    const items = within(menu).getAllByRole("menuitemradio");
    expect(items.map((b) => b.querySelector("strong")?.textContent)).toEqual(["Bu bilgisayar", "prod"]);
    expect(items[0].getAttribute("aria-checked")).toBe("true");
    // progress shows while it sets the server up
    bridge.ConnectRemote.mockImplementationOnce(async (json: string) => {
      act(() => handlers.get("rove:conn-progress")?.("installing rovecode on the server"));
      return { status: "connected", host: JSON.parse(json).host, http: "http://127.0.0.1:5555", token: "t" };
    });
    fireEvent.click(items[1]);
    await waitFor(() => expect(bridge.ConnectRemote).toHaveBeenCalledWith(JSON.stringify({ host: "prod", user: "", port: 0, keyPath: "" })));
    await waitFor(() => expect(reload).toHaveBeenCalled());
    // after the reload the app says where it runs
    act(() => handlers.get("rove:conn")?.({ status: "connected", host: "prod", http: "http://127.0.0.1:5555" }));
    await waitFor(() => expect(screen.getByRole("button", { name: /prod/ }).className).toContain("connected"));
  });

  it("a failed connection says why and stays on this computer", async () => {
    bridge.ConnectRemote.mockResolvedValueOnce({ status: "error", message: "cannot reach prod over SSH: Permission denied (publickey)" } as never);
    render(<ConnectionMenu lang="tr" onAddServer={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: /Bu bilgisayar/ }));
    fireEvent.click(within(screen.getByRole("menu")).getAllByRole("menuitemradio")[1]);
    await waitFor(() => expect(document.querySelector(".conn-err")?.textContent).toContain("Permission denied"));
    expect(reload).not.toHaveBeenCalled();
  });

  it("a dropped tunnel shows a banner until it is back", async () => {
    render(<ConnectionBanner lang="tr" />);
    await waitFor(() => expect(handlers.has("rove:conn")).toBe(true));
    act(() => handlers.get("rove:conn")?.({ status: "reconnecting", host: "prod", message: "attempt 2" }));
    expect(screen.getByRole("status").textContent).toContain("yeniden bağlanıyor · prod (attempt 2)");
    act(() => handlers.get("rove:conn")?.({ status: "connected", host: "prod" }));
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("on a server, picking a folder browses the server's folders", async () => {
    bridge.Connection.mockResolvedValue({ status: "connected", host: "prod" } as never);
    render(<FolderBrowserHost lang="tr" />);
    let picked = "";
    const done = pickFolder().then((p) => { picked = p; });
    const dialog = await screen.findByRole("dialog", { name: "Klasör seç" });
    await waitFor(() => expect(within(dialog).getAllByRole("button").some((b) => b.textContent?.includes("api"))).toBe(true));
    fireEvent.click(within(dialog).getAllByRole("button").find((b) => b.textContent?.trim() === "api")!);
    await waitFor(() => expect(dialog.querySelector(".folder-foot code")?.textContent).toBe("/home/u/api"));
    expect(dialog.querySelector(".folder-project")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Bu klasörü aç" }));
    await done;
    expect(picked).toBe("/home/u/api");
    bridge.Connection.mockResolvedValue({ status: "local" } as never);
  });

  it("warns when the daemon is older than the app", async () => {
    health = { apiLevel: 0 };
    render(<ApiBanner lang="tr" />);
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain(`API 0, bu uygulama ${REQUIRED_API} istiyor`));
    cleanup();
    // a different daemon answering: what the last one said no longer holds
    forgetReads();
    health = { apiLevel: REQUIRED_API };
    render(<ApiBanner lang="tr" />);
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
