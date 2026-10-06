import { describe, expect, it } from "vitest";
import { localizeCatalog } from "~/lib/catalogI18n";
import type { PersonaCatalog } from "~/lib/types";

const cat: PersonaCatalog = {
  characters: [{
    id: "uiux", name: "UI/UX Tasarımcı", category: "Tasarım", summary: "Kullanıcı akışı", role: "designer", features: [], prompt: "p",
    i18n: { en: { name: "UI/UX Designer", summary: "User flows", category: "Design" }, de: { name: "UI/UX-Designer", summary: "Nutzerflüsse", category: "Design" } },
  }],
  features: [{ key: "read", group: "tools", name: "Dosya okuma", desc: "Okur.", i18n: { en: { name: "Read files", summary: "Reads." } } }],
  defaults: [],
};

describe("catalog in the app's language", () => {
  it("uses the language's text, keeping ids and prompts", () => {
    const de = localizeCatalog(cat, "de").characters[0];
    expect([de.id, de.name, de.summary, de.category, de.prompt]).toEqual(["uiux", "UI/UX-Designer", "Nutzerflüsse", "Design", "p"]);
    expect(localizeCatalog(cat, "en").features[0]).toMatchObject({ key: "read", name: "Read files", desc: "Reads." });
  });
  it("falls back to English for a language without text, and Turkish is the base", () => {
    expect(localizeCatalog(cat, "ja").characters[0].name).toBe("UI/UX Designer");
    expect(localizeCatalog(cat, "ja").features[0].name).toBe("Read files");
    expect(localizeCatalog(cat, "tr")).toBe(cat);
  });
});
