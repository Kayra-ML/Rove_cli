import { describe, it, expect } from "vitest";
import { toast, subscribeToasts, dismissToast } from "../lib/toast";

describe("toast", () => {
  it("pushes and dismisses", () => {
    let seen: { id: number; text: string }[] = [];
    const unsub = subscribeToasts((list) => { seen = list; });
    toast("hello", "ok");
    expect(seen.some((t) => t.text === "hello")).toBe(true);
    dismissToast(seen[seen.length - 1].id);
    expect(seen.some((t) => t.text === "hello")).toBe(false);
    unsub();
  });
});
