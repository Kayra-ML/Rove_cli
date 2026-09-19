import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(
  resolve(__dirname, "../styles/global.css"),
  "utf8",
);
const app = readFileSync(
  resolve(__dirname, "../app/App.tsx"),
  "utf8",
);

describe("desktop left rail default-off", () => {
  it("collapses the left column instead of a 28px stub", () => {
    expect(css).toMatch(/\.body-side-off\s*\{\s*grid-template-columns:\s*1fr 260px/);
    expect(css).toMatch(/\.body-side-off\.body-aux-off\s*\{\s*grid-template-columns:\s*1fr 28px/);
    expect(css).toMatch(/\.body-market\.body-side-off\s*\{\s*grid-template-columns:\s*1fr/);
    expect(css).not.toMatch(/\.body-side-off\s*\{\s*grid-template-columns:\s*28px/);
  });

  it("hides the sidebar from the grid when collapsed", () => {
    expect(css).toMatch(/\.sidebar\[hidden\]/);
    expect(css).toMatch(/display:\s*none/);
  });

  it("defaults closed unless localStorage is 1", () => {
    expect(app).toContain('localStorage.getItem("aether.sideOpen") === "1"');
    expect(app).not.toContain('localStorage.getItem("aether.sideOpen") !== "0"');
    expect(app).not.toContain("side-show");
    expect(app).toContain('hidden={!sideOpen}');
  });
});
