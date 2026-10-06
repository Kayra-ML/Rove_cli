import type { CatalogText, PersonaCatalog } from "./types";

// The catalog's base text is Turkish; each entry carries its other languages
// in i18n. A language without an entry falls back to English, then Turkish.
function pick(i18n: Record<string, CatalogText> | undefined, lang: string): CatalogText | undefined {
  if (lang === "tr" || !i18n) return undefined;
  return i18n[lang] ?? i18n.en;
}

// localizeCatalog returns the catalog as the user reads it in `lang`. Ids,
// tags, prompts and features' keys stay as they are.
export function localizeCatalog(cat: PersonaCatalog, lang: string): PersonaCatalog {
  if (lang === "tr") return cat;
  return {
    ...cat,
    characters: cat.characters.map((c) => {
      const tx = pick(c.i18n, lang);
      return tx ? { ...c, name: tx.name, summary: tx.summary ?? c.summary, category: tx.category ?? c.category } : c;
    }),
    features: cat.features.map((f) => {
      const tx = pick(f.i18n, lang);
      return tx ? { ...f, name: tx.name, desc: tx.summary ?? f.desc } : f;
    }),
  };
}
