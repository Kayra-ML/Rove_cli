export type ThemeId =
  | "aether"
  | "obsidian"
  | "nord"
  | "catppuccin"
  | "tokyo"
  | "dracula"
  | "gruvbox"
  | "rose"
  | "solarized"
  | "onedark"
  | "github"
  | "paper"
  | "sand"
  | "forest"
  | "crimson"
  | "midnight";

export const THEMES: { id: ThemeId; label: string; swatch: [string, string, string] }[] = [
  { id: "aether",     label: "Aether",      swatch: ["#0b0c0e", "#8eb0ff", "#f2f4f7"] },
  { id: "obsidian",   label: "Obsidian",    swatch: ["#0a0a0a", "#c8c8c8", "#fafafa"] },
  { id: "nord",       label: "Nord",        swatch: ["#2e3440", "#88c0d0", "#eceff4"] },
  { id: "catppuccin", label: "Catppuccin",  swatch: ["#1e1e2e", "#cba6f7", "#cdd6f4"] },
  { id: "tokyo",      label: "Tokyo Night", swatch: ["#1a1b26", "#7aa2f7", "#c0caf5"] },
  { id: "dracula",    label: "Dracula",     swatch: ["#282a36", "#bd93f9", "#f8f8f2"] },
  { id: "gruvbox",    label: "Gruvbox",     swatch: ["#1d2021", "#fabd2f", "#ebdbb2"] },
  { id: "rose",       label: "Rosé Pine",   swatch: ["#191724", "#ebbcba", "#e0def4"] },
  { id: "solarized",  label: "Solarized",   swatch: ["#002b36", "#268bd2", "#eee8d5"] },
  { id: "onedark",    label: "One Dark",    swatch: ["#282c34", "#61afef", "#abb2bf"] },
  { id: "github",     label: "GitHub Dark", swatch: ["#0d1117", "#58a6ff", "#c9d1d9"] },
  { id: "paper",      label: "Paper",       swatch: ["#f6f4ef", "#1a1a1a", "#111"] },
  { id: "sand",       label: "Sand",        swatch: ["#f3ead3", "#6b4f2a", "#2a2114"] },
  { id: "forest",     label: "Forest",      swatch: ["#0f1a14", "#7dce94", "#e8f5e9"] },
  { id: "crimson",    label: "Crimson",     swatch: ["#14080c", "#ff5c7a", "#ffe8ec"] },
  { id: "midnight",   label: "Midnight",    swatch: ["#070b18", "#6ea8ff", "#e8eeff"] },
];

export function applyTheme(id: ThemeId) {
  document.documentElement.setAttribute("data-theme", id);
  localStorage.setItem("aether.theme", id);
  window.dispatchEvent(new CustomEvent("aether:prefs"));
}

export function applyLang(id: string) {
  document.documentElement.lang = id;
  document.documentElement.dir = id === "ar" ? "rtl" : "ltr";
  localStorage.setItem("aether.lang", id);
  window.dispatchEvent(new CustomEvent("aether:prefs"));
}

export function bootPrefs() {
  const theme = (localStorage.getItem("aether.theme") as ThemeId) || "aether";
  const lang = localStorage.getItem("aether.lang") || "tr";
  applyTheme(THEMES.some((t) => t.id === theme) ? theme : "aether");
  applyLang(lang);
}
