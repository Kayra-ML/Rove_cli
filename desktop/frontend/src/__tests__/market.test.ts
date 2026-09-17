import { describe, it, expect } from "vitest";

function isPlugin(s: { kind?: string }) {
  return s.kind === "plugin";
}

describe("market catalog split", () => {
  const catalog = [
    { manifest: { name: "web-design", description: "UI" }, kind: "plugin", plugin: "web-design" },
    { manifest: { name: "layout-composition", description: "grids" }, kind: "skill", plugin: "web-design" },
    { manifest: { name: "rust-core", description: "ownership" }, kind: "skill", plugin: "rust" },
    { manifest: { name: "rust", description: "systems" }, kind: "plugin", plugin: "rust" },
  ];

  it("splits 11-style plugins from skills", () => {
    const plugins = catalog.filter(isPlugin);
    const skills = catalog.filter((s) => !isPlugin(s));
    expect(plugins.map((p) => p.manifest.name)).toEqual(["web-design", "rust"]);
    expect(skills).toHaveLength(2);
  });

  it("groups skills by parent plugin", () => {
    const skills = catalog.filter((s) => !isPlugin(s));
    const map = new Map<string, string[]>();
    for (const s of skills) {
      const key = s.plugin || "other";
      map.set(key, [...(map.get(key) ?? []), s.manifest.name]);
    }
    expect(map.get("web-design")).toEqual(["layout-composition"]);
    expect(map.get("rust")).toEqual(["rust-core"]);
  });
});
