import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Settings → Appearance is one card of rows: themes as small tiles, the
// language as a select (not a wall of buttons), notifications as a switch.

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async () => []),
  discoverSSH: vi.fn(async () => []),
  pickFolder: vi.fn(),
  bridgeApp: vi.fn(() => undefined),
  resetConnection: vi.fn(),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { Settings } from "../app/Settings";
import { LANGS } from "../lib/i18n";
import { THEMES } from "../lib/themes";

describe("settings: appearance", () => {
  afterEach(() => { cleanup(); localStorage.clear(); });

  it("fits in one card: theme tiles, a language select, a notification switch", async () => {
    render(<Settings onClose={() => {}} initialSection="appearance" />);
    const themes = await screen.findByRole("radiogroup", { name: "Tema" });
    expect(within(themes).getAllByRole("radio")).toHaveLength(THEMES.length);
    expect(within(themes).getByRole("radio", { name: "Rove" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(within(themes).getByRole("radio", { name: "Nord" }));
    expect(within(themes).getByRole("radio", { name: "Nord" }).getAttribute("aria-checked")).toBe("true");
    expect(within(themes).getByRole("radio", { name: "Rove" }).getAttribute("aria-checked")).toBe("false");

    // the languages are one select, not a button per language
    const select = screen.getByRole("combobox", { name: "Dil" }) as HTMLSelectElement;
    expect(select.options).toHaveLength(LANGS.length);
    expect(screen.queryByRole("button", { name: "English" })).toBeNull();
    fireEvent.change(select, { target: { value: "en" } });

    expect(select.value).toBe("en");
    // the page speaks the chosen language right away
    const sw = await screen.findByRole("checkbox", { name: "Desktop notifications" }) as HTMLInputElement;
    expect(sw.checked).toBe(true);
    fireEvent.click(sw);
    expect(localStorage.getItem("aether.notify")).toBe("off");
    expect((screen.getByRole("button", { name: "Try" }) as HTMLButtonElement).disabled).toBe(true);
    expect(document.querySelectorAll(".set-card .set-row")).toHaveLength(3);
  });

  it("theme tiles are small and many to a row", () => {
    const css = readFileSync(resolve(__dirname, "../styles/global.css"), "utf8");
    expect(css).toMatch(/\.theme-mini \{ display: grid; grid-template-columns: repeat\(auto-fill, minmax\(92px, 1fr\)\)/);
    expect(css).not.toMatch(/\.lang-grid/);
  });
});
