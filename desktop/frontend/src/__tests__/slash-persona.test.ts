import { describe, expect, it } from "vitest";
import { filterSlash, matchSlash, parseGoal, SLASH } from "~/lib/slash";
import { matchCharacter } from "~/lib/agents";
import type { Character } from "~/lib/types";

describe("slash commands", () => {
  it("matches English and Turkish names with arguments", () => {
    expect(matchSlash("/harita auth")).toMatchObject({ cmd: { id: "map" }, rest: "auth" });
    expect(matchSlash("/bağlam")?.cmd.id).toBe("context");
    expect(matchSlash("/hedef giriş sayfası | testler geçer")).toMatchObject({ cmd: { id: "goal" }, rest: "giriş sayfası | testler geçer" });
    // Orchestra has no cast to add characters to
    expect(matchSlash("/ekip frontend")).toBeNull();
    expect(matchSlash("/hatırla API anahtarı .env'de")?.cmd.id).toBe("remember");
    expect(matchSlash("/kaydet önce")?.cmd.id).toBe("checkpoint");
    expect(matchSlash("/kullanım")?.cmd.id).toBe("usage");
  });

  // An agent is picked in the sidebar and stays with its chat: no switching
  // character, applying profiles or toggling features from a chat's
  // composer. Terminals are the exception: each picks its role (/character).
  it("has no persona, feature or character commands, in chats or terminals", () => {
    for (const cmd of ["/character go", "/karakter off", "/profil Backend", "/özellik", "/features"]) {
      expect(matchSlash(cmd, "chat")).toBeNull();
    }
    expect(SLASH.map((c) => c.id as string)).not.toContain("features");
    // the terminal has no characters either
    expect(matchSlash("/character go", "terminal")).toBeNull();
    expect(matchSlash("/rol frontend", "terminal")).toBeNull();
    // terminal-only and chat-only commands stay where they belong
    expect(matchSlash("/to 2 x", "chat")).toBeNull();
    expect(matchSlash("/team go", "terminal")).toBeNull();
    expect(filterSlash("/pa", "terminal").map((c) => c.id)).toEqual(["panes"]);
    expect(filterSlash("/pa", "chat")).toEqual([]);
  });

  it("offers the menu while typing the name, not the argument", () => {
    expect(filterSlash("/har").map((c) => c.id)).toEqual(["map"]);
    expect(filterSlash("/go").map((c) => c.id)).toEqual(["goal"]);
    expect(filterSlash("/map auth")).toEqual([]);
  });

  it("every command has a unique id and alias", () => {
    const aliases = SLASH.flatMap((c) => c.aliases);
    expect(new Set(aliases).size).toBe(aliases.length);
    expect(new Set(SLASH.map((c) => c.id)).size).toBe(SLASH.length);
  });
});

describe("/goal", () => {
  it("splits title and criteria; the title is the criterion when none is given", () => {
    expect(parseGoal("giriş sayfası | testler geçer, mobilde düzgün")).toEqual({ title: "giriş sayfası", criteria: ["testler geçer", "mobilde düzgün"] });
    expect(parseGoal("README yaz")).toEqual({ title: "README yaz", criteria: ["README yaz"] });
    expect(parseGoal("  | x")).toBeNull();
  });
});

describe("/team", () => {
  const ch = (id: string, name: string, tags: string[] = []): Character => ({ id, name, tags, category: "", summary: "", role: "developer", features: [], prompt: "" });
  const cat = [ch("frontend", "Frontend Uzmanı", ["react"]), ch("go-backend", "Go Backend Uzmanı", ["api"]), ch("uiux", "UI/UX Tasarımcı")];
  it("finds a character by id, name, prefix, tag or part of the name", () => {
    expect(matchCharacter(cat, "frontend")?.id).toBe("frontend");
    expect(matchCharacter(cat, "Go Backend Uzmanı")?.id).toBe("go-backend");
    expect(matchCharacter(cat, "go")?.id).toBe("go-backend");
    expect(matchCharacter(cat, "api")?.id).toBe("go-backend");
    expect(matchCharacter(cat, "tasarım")?.id).toBe("uiux");
    expect(matchCharacter(cat, "yok")).toBeNull();
  });
});
