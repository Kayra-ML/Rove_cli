import { describe, expect, it } from "vitest";
import { RIGHT, SIDE, columns, dragWidth, maxWidth } from "~/lib/layout";

describe("sidebar resize", () => {
  it("grows the left panel to the right and the right panel to the left", () => {
    expect(dragWidth("left", 250, 40, 380, 1600)).toBe(290);
    expect(dragWidth("right", 380, -60, 250, 1600)).toBe(440);
    expect(dragWidth("right", 380, 60, 250, 1600)).toBe(320);
  });

  it("clamps to each panel's own bound on a very wide screen", () => {
    // plenty of room and 32%/45% of the screen both exceed the fixed cap
    expect(dragWidth("left", 250, -500, 380, 1600)).toBe(SIDE.min);
    expect(dragWidth("left", 250, 900, 0, 3000)).toBe(SIDE.max);
    expect(dragWidth("right", 380, -2000, 0, 3000)).toBe(RIGHT.max);
  });

  it("caps a panel to a share of the window on an ordinary screen, tighter than its fixed max", () => {
    expect(maxWidth("left", 0, 1100)).toBe(Math.round(1100 * SIDE.maxPct));
    expect(maxWidth("left", 0, 1100)).toBeLessThan(SIDE.max);
    expect(dragWidth("left", 250, 900, 0, 1100)).toBe(Math.round(1100 * SIDE.maxPct));

    expect(maxWidth("right", 0, 1300)).toBe(Math.round(1300 * RIGHT.maxPct));
    expect(maxWidth("right", 0, 1300)).toBeLessThan(RIGHT.max);
    expect(dragWidth("right", 380, -2000, 0, 1300)).toBe(Math.round(1300 * RIGHT.maxPct));
  });

  it("never squeezes the center below its minimum, even under the percentage cap", () => {
    // 1000 wide, right panel 310: leftover room (306, with hairline
    // gutters) is tighter than both the fixed max and 32% of 1000
    expect(maxWidth("left", 310, 1000)).toBe(306);
    expect(dragWidth("left", 250, 900, 310, 1000)).toBe(306);
  });

  it("builds grid columns with gutters only between visible panels", () => {
    expect(columns(250, 380)).toBe("250px 1px minmax(0, 1fr) 1px 380px");
    expect(columns(null, 380)).toBe("minmax(0, 1fr) 1px 380px");
    expect(columns(250, null)).toBe("250px 1px minmax(0, 1fr)");
    expect(columns(null, null)).toBe("minmax(0, 1fr)");
  });
});
