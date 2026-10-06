import { describe, expect, it } from "vitest";
import { agentList, chatsByAgent } from "~/lib/agents";
import type { Character, PersonaBadge, Session } from "~/lib/types";

const s = (id: string): Session => ({ id, title: id, agentId: "a", workspaceId: "w", updatedAt: "" });
const ch = (id: string): Character => ({ id, name: id, category: "c", summary: "", role: "developer", features: [], prompt: "" });
const badges: Record<string, PersonaBadge> = { s1: { name: "Frontend", characterId: "frontend" }, s2: { name: "Ads", characterId: "marketing" }, s3: { name: "Frontend", characterId: "frontend" }, s4: { name: "custom" } };

describe("agents tab", () => {
  it("groups chats under the character they run as, keeping list order", () => {
    const g = chatsByAgent([s("s1"), s("s2"), s("s3"), s("s4"), s("s5")], (id) => badges[id] ?? null);
    expect(g.get("frontend")?.map((x) => x.id)).toEqual(["s1", "s3"]);
    expect(g.get("marketing")?.map((x) => x.id)).toEqual(["s2"]);
    expect(g.size).toBe(2); // chats without a character stay in the Sessions tab only
  });

  it("shows picked agents first, then any other agent that has chats", () => {
    const g = chatsByAgent([s("s1"), s("s2")], (id) => badges[id] ?? null);
    const list = agentList(["qa", "marketing", "gone"], g, [ch("frontend"), ch("marketing"), ch("qa")]);
    expect(list.map((c) => c.id)).toEqual(["qa", "marketing", "frontend"]);
  });
});
