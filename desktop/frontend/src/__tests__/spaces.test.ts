import { beforeEach, describe, expect, it } from "vitest";
import { adopt, DRAFT, freshDraft, LAST, loadModes, loadTop, modeFor, saveModes, spaceOf, withMode } from "~/lib/spaces";
import type { Session } from "~/lib/types";

const s = (id: string, space?: "office" | "chat"): Session => ({ id, title: id, agentId: "a", workspaceId: "w", updatedAt: "", space });

describe("spaces", () => {
  beforeEach(() => localStorage.clear());

  it("places a chat by its space, and older chats by whether they talk to an agent", () => {
    expect(spaceOf(s("1", "office"), null)).toBe("office");
    expect(spaceOf(s("2", "chat"), { name: "UI/UX", characterId: "uiux" })).toBe("chat");
    expect(spaceOf(s("3"), { name: "UI/UX", characterId: "uiux" })).toBe("office");
    expect(spaceOf(s("4"), null)).toBe("chat");
  });

  it("maps the old per-view modes onto the new spaces", () => {
    for (const [old, want] of [["agent", "chat"], ["terminal", "chat"], ["board", "chat"], ["map", "automation"], ["context", "automation"], ["chat", "chat"], [null, "office"]] as const) {
      if (old) localStorage.setItem("aether.mode", old); else localStorage.removeItem("aether.mode");
      expect(loadTop()).toBe(want);
    }
  });

  it("starts in Orchestra, then in the mode used last; each chat keeps its own", () => {
    expect(modeFor({}, "A")).toBe("orchestra");
    expect(modeFor({}, null)).toBe("orchestra");
    let m = withMode({}, "A", "teamwork");
    expect(modeFor(m, "A")).toBe("teamwork");
    // a chat without a mode of its own starts in the one used last
    expect(modeFor(m, "B")).toBe("teamwork");
    m = withMode(m, "B", "terminal");
    expect(modeFor(m, "A")).toBe("teamwork");
    expect(modeFor(m, "B")).toBe("terminal");
    expect(m[LAST]).toBe("terminal");
  });

  it("hands a draft's mode to the chat it becomes; a new draft starts in the last mode", () => {
    let m = withMode({}, null, "teamwork");
    expect(m[DRAFT]).toBe("teamwork");
    m = adopt(m, "D");
    expect(modeFor(m, "D")).toBe("teamwork");
    expect(m).not.toHaveProperty(DRAFT);
    m = withMode(m, null, "teamwork");
    m = withMode(m, "E", "terminal");
    m = freshDraft(m);
    expect(m).not.toHaveProperty(DRAFT);
    expect(modeFor(m, null)).toBe("terminal");
  });

  it("persists modes, reads the old chat mode as Orchestra, drops unknown ones", () => {
    saveModes({ A: "terminal" });
    expect(loadModes()).toEqual({ A: "terminal" });
    // a mode that no longer exists: those chats fall back to the default
    localStorage.setItem("aether.chat.modes", JSON.stringify({ A: "terminal", B: "spaceship", C: "chat" }));
    expect(loadModes()).toEqual({ A: "terminal", C: "orchestra" });
  });
});
