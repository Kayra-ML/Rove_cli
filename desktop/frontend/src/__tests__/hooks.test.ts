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
    onReconnect: vi.fn(() => () => {}),
    subscribeEvents: vi.fn().mockReturnValue(() => {}),
    hydrateConnection: vi.fn().mockResolvedValue({ token: "test", http: "http://localhost" }),
  };
});

import { act, renderHook, waitFor } from "@testing-library/react";
import { useAgents, useHistory } from "../hooks/useApi";
import { rpc } from "../lib/rpc";

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

  it("adds announced messages once, and keeps them past a read that missed them", async () => {
    const { result } = renderHook(() => useHistory("s1"));
    await waitFor(() => expect(result.current.messages).toHaveLength(2));
    const m3 = { id: "m3", sessionId: "s1", role: "tool" as const, content: "out", createdAt: "" };
    act(() => {
      result.current.push(m3);
      result.current.push(m3); // the same message twice is shown once
      result.current.push({ ...m3, id: "x", sessionId: "other" }); // another chat's
    });
    expect(result.current.messages.map((m) => m.id)).toEqual(["m1", "m2", "m3"]);
    // a read already on its way (answered before m4 was stored) must not drop it
    let answer!: (v: unknown) => void;
    vi.mocked(rpc).mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    let pending!: Promise<void>;
    act(() => { pending = result.current.reload(); });
    const m4 = { ...m3, id: "m4" };
    act(() => result.current.push(m4));
    await act(async () => {
      answer([{ id: "m1", sessionId: "s1", role: "user", content: "hello", createdAt: "" }]);
      await pending;
    });
    expect(result.current.messages.map((m) => m.id)).toEqual(["m1", "m4"]);
  });

  it("returns empty for null session", async () => {
    const { result } = renderHook(() => useHistory(null));
    await waitFor(() => expect(result.current.messages).toHaveLength(0));
  });
});
