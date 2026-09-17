import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(),
  subscribeEvents: vi.fn().mockReturnValue(() => {}),
  hydrateConnection: vi.fn(),
}));

vi.mock("../hooks/usePrefs", () => ({
  usePrefs: () => ({ lang: "en", theme: "aether" }),
}));

import { rpc } from "../lib/rpc";
import { UsageCard } from "../app/UsageCard";

describe("UsageCard", () => {
  beforeEach(() => {
    vi.mocked(rpc).mockResolvedValue({
      totalTokens: 12500,
      promptTokens: 8000,
      completionTokens: 4500,
      calls: 11,
      activeAgents: 2,
      days: 4,
      uptime: "3h2m",
    });
  });

  it("renders tokens, active agents and days from usage.get", async () => {
    render(<UsageCard />);
    await waitFor(() => expect(screen.getByText("12.5k")).toBeTruthy());
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
    expect(screen.getByText("tokens")).toBeTruthy();
    expect(screen.getByText("active agents")).toBeTruthy();
    expect(screen.getByText("days")).toBeTruthy();
    expect(rpc).toHaveBeenCalledWith("usage.get");
  });
});
