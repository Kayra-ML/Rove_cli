import { describe, expect, it } from "vitest";
import { initialsOf } from "~/lib/avatar";

describe("initialsOf", () => {
  it("takes the first letter of the first two real words", () => {
    expect(initialsOf("Frontend Uzmanı")).toBe("FU");
    expect(initialsOf("Go Backend Uzmanı")).toBe("GB");
    expect(initialsOf("DevOps & Altyapı")).toBe("DA");
    expect(initialsOf("Veri & ML Mühendisi")).toBe("VM");
  });

  it("falls back to two letters for a single word, and ? for empty", () => {
    expect(initialsOf("Karaktersiz")).toBe("KA");
    expect(initialsOf("   ")).toBe("?");
    expect(initialsOf("")).toBe("?");
  });
});
