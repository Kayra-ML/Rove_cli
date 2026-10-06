import { describe, expect, it } from "vitest";
import { bounds, groupColors, hash, seedPositions, tick, type SimNode } from "~/lib/forceGraph";
import { withAlpha } from "~/lib/neuralCanvas";
import { relTo } from "~/app/CodeMap";

function nodes(n: number, groups = 3): SimNode[] {
  return Array.from({ length: n }, (_, i) => ({ id: `f${i}.ts`, group: `g${i % groups}`, r: 5, x: 0, y: 0, vx: 0, vy: 0 }));
}

describe("forceGraph", () => {
  it("seeds deterministically and keeps known positions", () => {
    const a = nodes(20);
    const b = nodes(20);
    seedPositions(a);
    seedPositions(b);
    expect(a.map((n) => [n.x, n.y])).toEqual(b.map((n) => [n.x, n.y]));
    const c = nodes(20);
    seedPositions(c, new Map([["f3.ts", { x: 999, y: -5 }]]));
    expect(c[3].x).toBe(999);
    expect(c[3].y).toBe(-5);
  });

  it("pushes overlapping nodes apart and pulls linked ones together", () => {
    const ns = nodes(2, 1);
    ns[0].x = 0; ns[1].x = 1;
    for (let i = 0; i < 50; i++) tick(ns, [], 1);
    expect(Math.abs(ns[1].x - ns[0].x)).toBeGreaterThan(10);

    const far = nodes(2, 2);
    far[0].x = -800; far[1].x = 800;
    const before = far[1].x - far[0].x;
    for (let i = 0; i < 200; i++) tick(far, [{ s: 0, t: 1, w: 1 }], 1);
    expect(far[1].x - far[0].x).toBeLessThan(before);
  });

  it("does not move pinned nodes", () => {
    const ns = nodes(3, 1);
    seedPositions(ns);
    ns[0].pinned = true;
    const { x, y } = ns[0];
    for (let i = 0; i < 20; i++) tick(ns, [{ s: 0, t: 1, w: 1 }], 1);
    expect(ns[0].x).toBe(x);
    expect(ns[0].y).toBe(y);
  });

  it("gives the biggest group the first color", () => {
    const m = groupColors(["b", "a", "a", "a", "c", "c"]);
    expect([...m.keys()]).toEqual(["a", "c", "b"]);
  });

  it("hash is stable and bounds cover radii", () => {
    expect(hash("x")).toBe(hash("x"));
    expect(hash("x")).not.toBe(hash("y"));
    const b = bounds([{ id: "a", group: "", r: 2, x: 10, y: 0, vx: 0, vy: 0 }]);
    expect(b).toEqual({ x0: 8, y0: -2, x1: 12, y1: 2 });
  });
});

describe("map helpers", () => {
  it("withAlpha converts hex and rgb", () => {
    expect(withAlpha("#fff", 0.5)).toBe("rgba(255,255,255,0.5)");
    expect(withAlpha("#102030", 1)).toBe("rgba(16,32,48,1)");
    expect(withAlpha("rgb(1, 2, 3)", 0.2)).toBe("rgba(1,2,3,0.2)");
  });

  it("relTo maps agent paths onto node ids", () => {
    expect(relTo("/w/proj", "/w/proj/src/a.ts")).toBe("src/a.ts");
    expect(relTo("/w/proj/", "./src/a.ts")).toBe("src/a.ts");
    expect(relTo("/w/proj", "src/a.ts")).toBe("src/a.ts");
  });
});
