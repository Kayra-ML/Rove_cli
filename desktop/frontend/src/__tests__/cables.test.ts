import { describe, expect, it } from "vitest";
import { CARD_H, CARD_W, anchors, at, cable, cardRect, fitView, freeSpot, hitCard, overlaps, slide, spread } from "~/lib/cables";

describe("cables", () => {
  it("joins cards corner to corner, never from the middle of a side", () => {
    const a = cardRect({ x: 0, y: 0 });
    const isCorner = (p: { x: number; y: number }, r: typeof a) =>
      (p.x === r.x || p.x === r.x + r.w) && (p.y === r.y || p.y === r.y + r.h);
    // diagonal: a's bottom-right to b's top-left
    const b = cardRect({ x: 500, y: 300 });
    const d = anchors(a, b);
    expect(d.from).toEqual({ x: CARD_W, y: CARD_H });
    expect(d.to).toEqual({ x: 500, y: 300 });
    // side by side, level: the upper corners, a level cable
    const side = anchors(a, cardRect({ x: 500, y: 0 }));
    expect(side.from).toEqual({ x: CARD_W, y: 0 });
    expect(side.to).toEqual({ x: 500, y: 0 });
    // stacked: a bottom corner to a top corner
    const below = anchors(a, cardRect({ x: 0, y: 400 }));
    expect(isCorner(below.from, a) && below.from.y === CARD_H).toBe(true);
    expect(below.to.y).toBe(400);
    // whichever way round
    const back = anchors(b, a);
    expect(back.from).toEqual({ x: 500, y: 300 });
    expect(back.to).toEqual({ x: CARD_W, y: CARD_H });
  });

  it("is a straight line, and a point along it stays on it", () => {
    const c = cable({ x: 0, y: 0 }, { x: 400, y: 200 });
    expect(c.d).toBe("M 0 0 L 400 200");
    expect(at(c, 0)).toMatchObject({ x: 0, y: 0 });
    const mid = at(c, 0.5);
    expect(mid.x).toBeCloseTo(200);
    expect(mid.y).toBeCloseTo(100);
    expect(mid.angle).toBeCloseTo((Math.atan2(200, 400) * 180) / Math.PI);
    const end = at(c, 1);
    expect(end.x).toBeCloseTo(400);
    expect(end.y).toBeCloseTo(200);
  });

  it("hit-tests cards and skips the source", () => {
    const rects = new Map([["a", cardRect({ x: 0, y: 0 })], ["b", cardRect({ x: 300, y: 0 })]]);
    expect(hitCard({ x: 310, y: 10 }, rects)).toBe("b");
    expect(hitCard({ x: 10, y: 10 }, rects, "a")).toBeNull();
    expect(hitCard({ x: 280, y: 10 }, rects)).toBeNull();
  });

  it("fits all cards and avoids stacking drops", () => {
    const cam = fitView([cardRect({ x: 0, y: 0 }), cardRect({ x: 1000, y: 600 })], 800, 600);
    expect(cam.k).toBeGreaterThanOrEqual(0.3);
    expect(cam.k).toBeLessThanOrEqual(1.2);
    const spot = freeSpot({ x: 0, y: 0 }, [cardRect({ x: 0, y: 0 })]);
    expect(spot).not.toEqual({ x: 0, y: 0 });
    // not even partly on top of the one in the way
    const two = freeSpot({ x: 40, y: 30 }, [cardRect({ x: 0, y: 0 })]);
    expect(Math.abs(two.x) >= CARD_W || Math.abs(two.y) >= CARD_H).toBe(true);
  });

  it("spreads cables that leave one card at one spot along its edge", () => {
    const rects = new Map([["a", cardRect({ x: 0, y: 0 })], ["b", cardRect({ x: 400, y: 0 })]]);
    const mid = { x: CARD_W, y: CARD_H / 2 };
    const out = spread([["a", mid], ["b", { x: 400, y: CARD_H / 2 }], ["a", mid], ["b", { x: 400, y: CARD_H / 2 + 30 }]], rects);
    // the two ends on a's right edge move apart, up and down, still on the edge
    expect(out[0].x).toBe(CARD_W);
    expect(out[2].x).toBe(CARD_W);
    expect(Math.abs(out[0].y - out[2].y)).toBe(14);
    // ends with a spot of their own stay put
    expect(out[1]).toEqual({ x: 400, y: CARD_H / 2 });
    expect(out[3]).toEqual({ x: 400, y: CARD_H / 2 + 30 });
  });

  it("never lets a dragged card into another: it slides along, or stays", () => {
    const other = cardRect({ x: 300, y: 0 });
    // straight into it: stopped where it was
    expect(slide({ x: 0, y: 0 }, { x: 200, y: 0 }, [other])).toEqual({ x: 0, y: 0 });
    // into it on a slant: keeps the part of the move that stays clear
    expect(slide({ x: 0, y: 0 }, { x: 200, y: 50 }, [other])).toEqual({ x: 0, y: 50 });
    // clear: goes where it is pointed
    expect(slide({ x: 0, y: 0 }, { x: 0, y: 200 }, [other])).toEqual({ x: 0, y: 200 });
    // a card already on another can be pulled off it
    expect(slide({ x: 310, y: 10 }, { x: 320, y: 20 }, [other])).toEqual({ x: 320, y: 20 });
    expect(overlaps(cardRect({ x: 0, y: 0 }), other)).toBe(false);
  });
});
