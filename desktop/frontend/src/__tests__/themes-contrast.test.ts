import { describe, expect, it } from "vitest";
import { PALETTES, THEMES, applyTheme, bootPrefs, isLightTheme, type ThemeId } from "../lib/themes";

// Every theme must read clearly: text and secondary text on each surface,
// labels on accent buttons, and a side panel that is a surface of its own
// (not the same color as the work area).

function lum(hex: string): number {
  const h = hex.replace("#", "");
  const n = h.length === 3 ? h.split("").map((c) => c + c).join("") : h;
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(n.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
export function contrast(a: string, b: string): number {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

const ids = THEMES.map((t) => t.id);

describe("themes", () => {
  it("every listed theme has a palette, and some are light", () => {
    expect(new Set(ids)).toEqual(new Set(Object.keys(PALETTES)));
    expect(ids.filter((id) => isLightTheme(id)).length).toBeGreaterThanOrEqual(4);
  });

  it.each(ids)("%s: text reads on every surface", (id) => {
    const p = PALETTES[id as ThemeId];
    const fails: string[] = [];
    const need = (label: string, fg: string, bg: string, min: number) => {
      const c = contrast(fg, bg);
      if (c < min) fails.push(`${label} ${fg} on ${bg} = ${c.toFixed(2)} < ${min}`);
    };
    for (const [name, bg] of [["raised", p.raised], ["hover", p.hover], ["sunken", p.sunken], ["bg", p.bg]] as const) {
      need(`text/${name}`, p.text, bg, 7);
      need(`muted/${name}`, p.muted, bg, 4.5);
      need(`faint/${name}`, p.faint, bg, 3);
    }
    need("accent/raised", p.accent, p.raised, 3);
    // syntax colouring: comments are `faint` (checked above), the rest sit
    // on the code block's own background
    for (const c of ["accent", "accent2", "ok", "warn"] as const) need(`syntax ${c}/sunken`, p[c], p.sunken, 3);
    need("onAccent/accent", p.onAccent, p.accent, 4.5);
    for (const c of ["ok", "warn", "bad"] as const) need(`${c}/raised`, p[c], p.raised, 3);
    for (const [name, bg] of [["side", p.side.bg], ["side-hover", p.side.hover]] as const) {
      need(`side-text/${name}`, p.side.text, bg, 7);
      need(`side-muted/${name}`, p.side.muted, bg, 4.5);
      need(`side-faint/${name}`, p.side.faint, bg, 3);
      need(`side-accent/${name}`, p.side.accent, bg, 3);
    }
    expect(fails).toEqual([]);
  });

  it.each(ids)("%s: the side panel is its own color", (id) => {
    const p = PALETTES[id as ThemeId];
    expect(p.side.bg.toLowerCase()).not.toBe(p.raised.toLowerCase());
    expect(contrast(p.side.bg, p.raised)).toBeGreaterThan(1.05);
  });

  it("applying a theme sets its colors and drops the last theme's extras", () => {
    applyTheme("graphite");
    const s = document.documentElement.style;
    expect(s.getPropertyValue("--side-bg")).toBe(PALETTES.graphite.side.bg);
    expect(s.getPropertyValue("--r")).toBe("4px");
    applyTheme("paper");
    expect(s.getPropertyValue("--accent")).toBe(PALETTES.paper.accent);
    expect(s.getPropertyValue("--on-accent")).toBe("#ffffff");
    expect(s.getPropertyValue("--r")).toBe("");
    expect(document.documentElement.dataset.theme).toBe("paper");
  });

  it("someone on a dropped theme gets the nearest one kept", () => {
    localStorage.setItem("aether.theme", "dracula");
    bootPrefs();
    expect(document.documentElement.dataset.theme).toBe("tokyo");
    localStorage.setItem("aether.theme", "no-such-theme");
    bootPrefs();
    expect(document.documentElement.dataset.theme).toBe("graphite");
  });
});
