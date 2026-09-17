import { describe, it, expect } from "vitest";
import { compact } from "../app/UsageCard";

describe("compact tokens", () => {
  it("formats buckets", () => {
    expect(compact(0)).toBe("0");
    expect(compact(12)).toBe("12");
    expect(compact(12500)).toBe("12.5k");
    expect(compact(1000)).toBe("1k");
    expect(compact(1_500_000)).toBe("1.5M");
    expect(compact(-3)).toBe("0");
  });
});
