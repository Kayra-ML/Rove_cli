import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Every Settings section uses the Appearance cards: lists as one card,
// forms with the label on the left and the field on the right.

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string) => {
    switch (method) {
      case "provider.list": return [{ id: "p1", name: "qwen", kind: "openai-compat", baseUrl: "https://x/v1", models: ["qwen3-max"], default: true }];
      case "workspace.list": return [{ id: "w", name: "proj", path: "/p", defaultBranch: "main" }];
      case "goal.list": return [{ id: "g", title: "README", status: "blocked", iteration: 3 }];
      case "mcp.list": return [];
      default: return [];
    }
  }),
  discoverSSH: vi.fn(async () => []),
  pickFolder: vi.fn(),
  bridgeApp: vi.fn(() => undefined),
  resetConnection: vi.fn(),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { Settings } from "../app/Settings";

// a form's grid: each label is followed by its field
function pairs(form: Element) {
  const kids = Array.from(form.children);
  return kids.filter((k) => k.tagName === "LABEL").map((l) => kids[kids.indexOf(l) + 1]?.tagName);
}

describe("settings: every section in cards", () => {
  afterEach(cleanup);

  for (const [section, name] of [["providers", "Sağlayıcılar"], ["workspace", "Çalışma alanları"], ["goals", "Hedefler"], ["servers", "Sunucular"], ["mcp", "MCP"]] as const) {
    it(`${section}: forms pair each label with its field`, async () => {
      render(<Settings onClose={() => {}} initialSection={section} />);
      await waitFor(() => expect(document.querySelector(".split-body h2")).toBeTruthy());
      for (const form of Array.from(document.querySelectorAll(".split-body .form-stack"))) {
        for (const tag of pairs(form)) expect(["INPUT", "SELECT", "TEXTAREA"]).toContain(tag);
      }
      void name;
    });
  }

  it("the goals form has a label for each field and its button inside the card", async () => {
    render(<Settings onClose={() => {}} initialSection="goals" />);
    await screen.findByText("README");
    const form = document.querySelector(".split-body .form-stack")!;
    expect(Array.from(form.querySelectorAll("label")).map((l) => l.textContent)).toEqual(["Başlık", "Kriterler"]);
    expect(form.querySelector("button.primary")).toBeTruthy();
    fireEvent.change(form.querySelectorAll("input")[0], { target: { value: "x" } });
  });

  it("styles: lists are one card, forms two columns, later headings are subtitles", () => {
    const css = readFileSync(resolve(__dirname, "../styles/global.css"), "utf8");
    expect(css).toMatch(/\.split-body \.row \+ \.row \{ border-top: 1px solid var\(--border\); \}/);
    expect(css).toMatch(/\.split-body \.form-stack \{\s*display: grid; grid-template-columns: minmax\(110px, 180px\) minmax\(0, 1fr\)/);
    expect(css).toMatch(/\.split-body h2 ~ h2 \{ margin-top: 30px; font-size: 14px/);
  });
});
