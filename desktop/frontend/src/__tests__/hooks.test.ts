import { describe, it, expect, vi } from "vitest";

// Mock the RPC layer before importing hooks
vi.mock("../lib/rpc", () => {
  const responses: Record<string, unknown> = {
    "agent.list": [
      {
        id: "a1", name: "default", provider: "fake", model: "fake",
        status: "idle", profile: "t", workspaceId: "",
      },
    ],
    "session.list": [
      { id: "s1", title: "t", agentId: "a1", workspaceId: "", updatedAt: "" },
    ],
    "session.history": [
      { id: "m1", sessionId: "s1", role: "user", content: "hello", createdAt: "" },
      { id: "m2", sessionId: "s1", role: "assistant", content: "world", createdAt: "" },
    ],
    "card.list": [
      { id: "c1", title: "task", column: "ready", status: "", description: "",
        profile: "", model: "", goalMode: false, workspaceId: "" },
    ],
    "goal.list": [],
    "workspace.list": [],
    "skill.list": [],
    "provider.list": [],
    "terminal.list": [],
  };
  return {
    rpc: vi.fn().mockImplementation((method: string) =>
      Promise.resolve(responses[method] ?? null),
    ),
    subscribeEvents: vi.fn().mockReturnValue(() => {}),
    hydrateConnection: vi.fn().mockResolvedValue({ token: "test", http: "http://localhost" }),
  };
});

import { renderHook, waitFor } from "@testing-library/react";
import { useAgents, useHistory, useCards } from "../hooks/useApi";

describe("useAgents", () => {
  it("loads agents", async () => {
    const { result } = renderHook(() => useAgents());
    await waitFor(() => expect(result.current.agents).toHaveLength(1));
    expect(result.current.agents[0].name).toBe("default");
  });
});

describe("useHistory", () => {
  it("loads messages for session", async () => {
    const { result } = renderHook(() => useHistory("s1"));
    await waitFor(() => expect(result.current.messages).toHaveLength(2));
    expect(result.current.messages[0].role).toBe("user");
  });

  it("returns empty for null session", async () => {
    const { result } = renderHook(() => useHistory(null));
    await waitFor(() => expect(result.current.messages).toHaveLength(0));
  });
});

describe("useCards", () => {
  it("loads cards", async () => {
    const { result } = renderHook(() => useCards());
    await waitFor(() => expect(result.current.cards).toHaveLength(1));
    expect(result.current.cards[0].column).toBe("ready");
  });
});