import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { UsageReport } from "../app/UsagePanel";

const calls: [string, unknown][] = [];
let report: UsageReport;
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    return method === "usage.report" ? report : null;
  }),
  subscribeEvents: vi.fn(() => () => {}),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { UsagePanel, niceTicks } from "../app/UsagePanel";

const zero = { calls: 0, sentTokens: 0, recvTokens: 0, sentChars: 0, recvChars: 0, reportedCalls: 0, reportedIn: 0, reportedOut: 0, cached: 0, oursOnReported: 0 };

describe("Settings → Usage", () => {
  beforeEach(() => {
    calls.length = 0;
    report = {
      since: "2026-09-30", until: "2026-10-02",
      days: [
        { ...zero, date: "2026-09-30", calls: 4, sentTokens: 12000, recvTokens: 3000, reportedCalls: 4, reportedIn: 40000, reportedOut: 5000, oursOnReported: 15000 },
        { ...zero, date: "2026-10-01" },
        { ...zero, date: "2026-10-02", calls: 2, sentTokens: 6000, recvTokens: 1000 },
      ],
      models: [
        { ...zero, provider: "claude-code", model: "opus", calls: 4, sentTokens: 12000, recvTokens: 3000, reportedCalls: 4, reportedIn: 40000, reportedOut: 5000, oursOnReported: 15000 },
        { ...zero, provider: "qwen", model: "flash", calls: 2, sentTokens: 6000, recvTokens: 1000 },
      ],
      total: { ...zero, calls: 6, sentTokens: 18000, recvTokens: 4000, sentChars: 70000, recvChars: 15000, reportedCalls: 4, reportedIn: 40000, reportedOut: 5000, cached: 20000, oursOnReported: 15000 },
    };
  });
  afterEach(cleanup);

  it("leads with Rove's own count, and sets the provider's word beside it", async () => {
    render(<UsagePanel />);
    await screen.findByText("Toplam token");
    await waitFor(() => expect(calls[0]).toEqual(["usage.report", { days: 30 }]));
    const tile = (label: string) => screen.getByText(label).closest(".usage-tile")!;
    expect(tile("Toplam token").querySelector(".usage-tile-value")?.textContent).toBe("22k");
    expect(tile("Toplam token").textContent).toContain("Rove'un sayımı");
    // two days in use: the average is over those, not over the empty one
    expect(tile("Günlük ortalama").querySelector(".usage-tile-value")?.textContent).toBe("11k");
    // the provider says three times what passed through Rove on its calls
    const cmp = document.querySelector(".usage-compare")!;
    expect(cmp.className).toContain("more");
    expect(cmp.querySelector(".usage-diff")?.textContent).toBe("+200%");
    expect(cmp.textContent).toContain("gizli istemleri");
    expect(cmp.textContent).toContain("4/6 çağrı");
    // a model that reported nothing says so, rather than showing a zero
    const rows = Array.from(document.querySelectorAll(".usage-models tbody tr")).map((r) => r.textContent);
    expect(rows[1]).toContain("bildirmedi");
  });

  it("draws a bar a day — sent and received stacked — and offers a table", async () => {
    render(<UsagePanel />);
    await screen.findByText("Günlere göre");
    // two days with calls: two sent segments and two received ones; the quiet day has none
    expect(document.querySelectorAll(".usage-bar.sent")).toHaveLength(2);
    expect(document.querySelectorAll(".usage-bar.recv")).toHaveLength(2);
    // every day still has its hover target and tooltip
    expect(document.querySelectorAll(".usage-hit")).toHaveLength(3);
    fireEvent.mouseEnter(document.querySelectorAll(".usage-hit")[0]);
    expect(document.querySelector(".usage-tip")?.textContent).toContain("2026-09-30");
    fireEvent.click(screen.getByRole("button", { name: "Tablo olarak" }));
    const days = Array.from(document.querySelectorAll(".usage-chart-wrap tbody tr"));
    expect(days.map((r) => r.querySelector("td")?.textContent)).toEqual(["2026-10-02", "2026-10-01", "2026-09-30"]);
    expect(days[1].className).toContain("quiet");
  });

  it("switches the range", async () => {
    render(<UsagePanel />);
    await screen.findByText("Toplam token");
    await act(async () => { fireEvent.click(screen.getByRole("tab", { name: "7 gün" })); });
    await waitFor(() => expect(calls.some(([m, p]) => m === "usage.report" && (p as { days: number }).days === 7)).toBe(true));
  });

  it("an axis of round steps past the top", () => {
    expect(niceTicks(15000)).toEqual([0, 5000, 10000, 15000]);
    expect(niceTicks(22000)).toEqual([0, 10000, 20000, 30000]);
    expect(niceTicks(1)).toEqual([0, 0.25, 0.5, 0.75, 1]);
  });
});
