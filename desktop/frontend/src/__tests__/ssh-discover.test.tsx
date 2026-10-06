import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

// Settings → Servers: "find servers on this computer" lists ~/.ssh/config
// and known_hosts entries; the picked ones are added in one go.

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async () => []),
  discoverSSH: vi.fn(async () => [
    { alias: "prod", hostName: "10.0.0.5", user: "deploy", port: 2222, source: "config" },
    { alias: "lab", hostName: "192.168.1.20", source: "config" },
    { hostName: "203.0.113.9", port: 2200, source: "known_hosts" },
  ]),
  pickFolder: vi.fn(),
  bridgeApp: vi.fn(() => undefined),
  resetConnection: vi.fn(),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite", setTheme: vi.fn(), setLang: vi.fn() }) }));

import { Settings } from "../app/Settings";
import { loadSSHHosts } from "../app/Popovers";

describe("ssh discovery", () => {
  afterEach(() => { cleanup(); localStorage.clear(); });

  it("finds this computer's servers and adds the picked ones", async () => {
    localStorage.setItem("aether.sshHosts", JSON.stringify([{ host: "lab", hostName: "192.168.1.20", authMethod: "agent" }]));
    render(<Settings onClose={() => {}} initialSection="servers" />);
    fireEvent.click(await screen.findByRole("button", { name: /Bu bilgisayardaki sunucuları bul/ }));
    const found = await screen.findByRole("group", { name: "Bulunan sunucular" });
    const rows = () => Array.from(found.querySelectorAll(".ssh-found-row")) as HTMLElement[];
    expect(rows().map((r) => r.querySelector("strong")?.textContent)).toEqual(["prod", "lab", "203.0.113.9"]);
    expect(rows()[0].textContent).toContain("deploy@10.0.0.5:2222");
    // the one already added is marked and cannot be picked twice
    const lab = rows()[1].querySelector("input") as HTMLInputElement;
    expect(lab.disabled).toBe(true);
    expect(rows()[1].textContent).toContain("Zaten ekli");
    // new config entries come ticked; a bare known_hosts entry does not
    expect((rows()[0].querySelector("input") as HTMLInputElement).checked).toBe(true);
    expect((rows()[2].querySelector("input") as HTMLInputElement).checked).toBe(false);
    fireEvent.click(rows()[2].querySelector("input")!);
    fireEvent.click(within(found).getByRole("button", { name: /Seçilenleri ekle \(2\)/ }));
    await waitFor(() => expect(screen.queryByRole("group", { name: "Bulunan sunucular" })).toBeNull());
    const saved = loadSSHHosts();
    // an alias stays an alias (ssh applies its config); a known host keeps its port
    expect(saved.find((h) => h.host === "prod")).toMatchObject({ host: "prod", hostName: "10.0.0.5" });
    expect(saved.find((h) => h.host === "prod")?.port).toBeUndefined();
    expect(saved.find((h) => h.host === "203.0.113.9")).toMatchObject({ port: 2200 });
    expect(saved).toHaveLength(3);
  });
});
