export type ThemeId =
  | "graphite"
  | "daylight"
  | "nord"
  | "catppuccin"
  | "tokyo"
  | "gruvbox"
  | "rose"
  | "onedark"
  | "github"
  | "paper"
  | "latte"
  | "github-light";

// Themes that were dropped, and the one each person who had picked it gets
// instead: the nearest in tone.
const RETIRED: Record<string, ThemeId> = {
  aether: "graphite", obsidian: "graphite", crimson: "graphite",
  dracula: "tokyo", midnight: "tokyo", solarized: "nord", forest: "nord",
  sand: "paper",
};

// A theme is two surfaces that talk to each other: the side panels (the
// session list, the settings rail) and the work area (chats, terminals,
// cards). Each has its own background and its own text colors, so a light
// work area can sit next to a dark or tinted side and both stay readable.
export interface Palette {
  light: boolean;
  bg: string;       // window chrome: the title bar and what frames the work
  // the work area itself (chat, maps, settings body); left out, it is
  // the raised surface
  panel?: string;
  raised: string;   // panels, cards, inputs
  hover: string;
  sunken: string;   // terminal, code, wells
  border: string;
  text: string;
  muted: string;    // secondary text: must read on every surface
  faint: string;    // hints and placeholders
  accent: string;   // links, focus, primary buttons
  accent2: string;  // running, secondary highlights
  onAccent: string; // text on an accent-filled button
  ok: string;
  warn: string;
  bad: string;
  side: { bg: string; hover: string; border: string; text: string; muted: string; faint: string; accent: string };
  // the title bar; left out, it is the frame (bg) with the work area's text
  bar?: { bg: string; hover: string; border: string; text: string; muted: string };
  extra?: Record<string, string>;
}

export const PALETTES: Record<ThemeId, Palette> = {
  // Rove: the brand is black and white, so the accent is too — white on
  // graphite. Four steps of grey told apart at a glance, none of them
  // black: frame and side list share the darkest, the work area sits one
  // step up, cards and menus one more, code and terminal wells one down.
  graphite: {
    light: false, bg: "#151518", panel: "#1b1b1f", raised: "#222227", hover: "#2a2a30", sunken: "#121215", border: "#2e2e35",
    text: "#e4e4e9", muted: "#a2a2ae", faint: "#7a7a86", accent: "#ececf0", accent2: "#b9b9c3", onAccent: "#131316",
    ok: "#5fd394", warn: "#e9a862", bad: "#f17d7d",
    side: { bg: "#151518", hover: "#212126", border: "#2a2a30", text: "#e4e4e9", muted: "#a2a2ae", faint: "#7a7a86", accent: "#ececf0" },
    extra: { "--mono": '"JetBrains Mono", "SF Mono", "IBM Plex Mono", ui-monospace, monospace', "--r": "4px" },
  },
  // The named themes keep their own colors, laid out the way Rove is: the
  // frame and the side list share the darkest tone, the work area sits one
  // step up, cards and menus one more, code wells below the frame. Light
  // themes keep a dark title bar, so the mark reads as it is drawn.
  nord: {
    light: false, bg: "#272c36", panel: "#2e3440", raised: "#3b4252", hover: "#434c5e", sunken: "#242933", border: "#434c5e",
    text: "#eceff4", muted: "#d0d6e1", faint: "#a2b0c6", accent: "#88c0d0", accent2: "#b48ead", onAccent: "#232831",
    ok: "#a3be8c", warn: "#ebcb8b", bad: "#e0848c",
    side: { bg: "#272c36", hover: "#323845", border: "#353b47", text: "#eceff4", muted: "#c3cbd9", faint: "#8f9cb3", accent: "#88c0d0" },
  },
  catppuccin: {
    light: false, bg: "#181825", panel: "#1e1e2e", raised: "#252536", hover: "#313244", sunken: "#11111b", border: "#363849",
    text: "#cdd6f4", muted: "#a6adc8", faint: "#7f849c", accent: "#cba6f7", accent2: "#89b4fa", onAccent: "#1e1e2e",
    ok: "#a6e3a1", warn: "#f9e2af", bad: "#f38ba8",
    side: { bg: "#181825", hover: "#24253a", border: "#26263a", text: "#cdd6f4", muted: "#a6adc8", faint: "#7f849c", accent: "#cba6f7" },
  },
  tokyo: {
    light: false, bg: "#16161e", panel: "#1a1b26", raised: "#1f2335", hover: "#292e42", sunken: "#13131a", border: "#2f344d",
    text: "#c0caf5", muted: "#a9b1d6", faint: "#7a82ad", accent: "#7aa2f7", accent2: "#bb9af7", onAccent: "#15161e",
    ok: "#9ece6a", warn: "#e0af68", bad: "#f7768e",
    side: { bg: "#16161e", hover: "#20222f", border: "#24263a", text: "#c0caf5", muted: "#a2aad0", faint: "#747ca5", accent: "#7aa2f7" },
  },
  gruvbox: {
    light: false, bg: "#1d2021", panel: "#282828", raised: "#32302f", hover: "#3c3836", sunken: "#191b1c", border: "#45403d",
    text: "#ebdbb2", muted: "#bdae93", faint: "#a89984", accent: "#fabd2f", accent2: "#fe8019", onAccent: "#1d2021",
    ok: "#b8bb26", warn: "#fabd2f", bad: "#fb4934",
    side: { bg: "#1d2021", hover: "#2a2827", border: "#32302f", text: "#ebdbb2", muted: "#bdae93", faint: "#928374", accent: "#fabd2f" },
  },
  rose: {
    light: false, bg: "#16141f", panel: "#191724", raised: "#1f1d2e", hover: "#26233a", sunken: "#13111d", border: "#2e2b42",
    text: "#e0def4", muted: "#aeaac6", faint: "#817d9c", accent: "#ebbcba", accent2: "#c4a7e7", onAccent: "#191724",
    ok: "#9ccfd8", warn: "#f6c177", bad: "#eb6f92",
    side: { bg: "#16141f", hover: "#221f33", border: "#24213a", text: "#e0def4", muted: "#a9a5c2", faint: "#7c7896", accent: "#ebbcba" },
  },
  onedark: {
    light: false, bg: "#21252b", panel: "#282c34", raised: "#2c313a", hover: "#353b45", sunken: "#1e2227", border: "#3a3f4b",
    text: "#d7dae0", muted: "#a0a7b4", faint: "#838a99", accent: "#61afef", accent2: "#c678dd", onAccent: "#1e2227",
    ok: "#98c379", warn: "#e5c07b", bad: "#e06c75",
    side: { bg: "#21252b", hover: "#2c313a", border: "#2c313a", text: "#d7dae0", muted: "#9da5b4", faint: "#7d8595", accent: "#61afef" },
  },
  github: {
    light: false, bg: "#010409", panel: "#0d1117", raised: "#161b22", hover: "#1f2630", sunken: "#010409", border: "#30363d",
    text: "#e6edf3", muted: "#9ba5b0", faint: "#7d8590", accent: "#58a6ff", accent2: "#d2a8ff", onAccent: "#0d1117",
    ok: "#3fb950", warn: "#d29922", bad: "#f85149",
    side: { bg: "#010409", hover: "#151b23", border: "#21262d", text: "#e6edf3", muted: "#9ba5b0", faint: "#78818c", accent: "#58a6ff" },
  },
  // ── light ──
  paper: {
    light: true, bg: "#ebe6da", panel: "#fffdf8", raised: "#fffdf8", hover: "#f3eee3", sunken: "#f7f4ec", border: "#ddd5c5",
    text: "#1c1b18", muted: "#524d45", faint: "#736c60", accent: "#2f5bd3", accent2: "#8a4fd1", onAccent: "#ffffff",
    ok: "#1f7a45", warn: "#9a6200", bad: "#bf3326",
    side: { bg: "#f3efe5", hover: "#e7e0d1", border: "#ddd5c5", text: "#1c1b18", muted: "#4d473e", faint: "#6d6557", accent: "#2f5bd3" },
    bar: { bg: "#1c1b18", hover: "#2c2a26", border: "#2c2a26", text: "#efeae0", muted: "#b5ad9e" },
  },
  latte: {
    light: true, bg: "#dce0e8", panel: "#f8f9fb", raised: "#ffffff", hover: "#eceef3", sunken: "#eff1f5", border: "#d3d7e0",
    text: "#383a52", muted: "#55586f", faint: "#717489", accent: "#7329d9", accent2: "#1e66f5", onAccent: "#ffffff",
    ok: "#2f7d1f", warn: "#a85c05", bad: "#d20f39",
    side: { bg: "#e6e9ef", hover: "#d8dce5", border: "#d3d7e0", text: "#34364d", muted: "#4d5068", faint: "#6c6f85", accent: "#7329d9" },
    bar: { bg: "#1e1e2e", hover: "#2a2b3c", border: "#313244", text: "#eff1f5", muted: "#a6adc8" },
  },
  "github-light": {
    light: true, bg: "#eaeef2", panel: "#ffffff", raised: "#ffffff", hover: "#f3f5f8", sunken: "#f6f8fa", border: "#d0d7de",
    text: "#1f2328", muted: "#59636e", faint: "#6e7781", accent: "#0969da", accent2: "#8250df", onAccent: "#ffffff",
    ok: "#1a7f37", warn: "#9a6700", bad: "#cf222e",
    side: { bg: "#f6f8fa", hover: "#eaeef2", border: "#d8dee4", text: "#1f2328", muted: "#59636e", faint: "#6e7781", accent: "#0969da" },
    bar: { bg: "#1f2328", hover: "#2d333b", border: "#30363d", text: "#f0f3f6", muted: "#9ba5b0" },
  },
  // Rove Light: white work, a cool grey side list, black as the accent —
  // and the title bar stays black, so the mark reads as it is drawn.
  daylight: {
    light: true, bg: "#ececf0", panel: "#ffffff", raised: "#ffffff", hover: "#f0f0f3", sunken: "#f6f6f8", border: "#dcdce2",
    text: "#1b1b1f", muted: "#55555e", faint: "#74747d", accent: "#18181b", accent2: "#55555e", onAccent: "#ffffff",
    ok: "#17803d", warn: "#b0550a", bad: "#d42a2a",
    side: { bg: "#f3f3f5", hover: "#e7e7eb", border: "#dcdce2", text: "#1b1b1f", muted: "#55555e", faint: "#74747d", accent: "#18181b" },
    bar: { bg: "#131315", hover: "#232327", border: "#26262b", text: "#ececf0", muted: "#a2a2ae" },
  },
};

export const THEMES: { id: ThemeId; label: string }[] = [
  { id: "graphite",     label: "Rove" },
  { id: "daylight",     label: "Rove Light" },
  { id: "nord",         label: "Nord" },
  { id: "catppuccin",   label: "Catppuccin" },
  { id: "tokyo",        label: "Tokyo Night" },
  { id: "gruvbox",      label: "Gruvbox" },
  { id: "rose",         label: "Rosé Pine" },
  { id: "onedark",      label: "One Dark" },
  { id: "github",       label: "GitHub Dark" },
  { id: "paper",        label: "Paper" },
  { id: "latte",        label: "Latte" },
  { id: "github-light", label: "GitHub Light" },
];

// The CSS custom properties a palette sets on <html>.
function themeVars(p: Palette): Record<string, string> {
  return {
    "--bg": p.bg, "--panel": p.panel ?? p.raised, "--bg-raised": p.raised, "--bg-hover": p.hover, "--bg-sunken": p.sunken,
    "--border": p.border, "--text": p.text, "--muted": p.muted, "--faint": p.faint,
    "--accent": p.accent, "--accent-2": p.accent2, "--on-accent": p.onAccent,
    "--ok": p.ok, "--warn": p.warn, "--bad": p.bad,
    "--col-backlog": p.muted, "--col-ready": p.accent, "--col-running": p.accent2,
    "--col-review": p.warn, "--col-done": p.ok, "--col-blocked": p.bad,
    "--side-bg": p.side.bg, "--side-hover": p.side.hover, "--side-border": p.side.border,
    "--side-text": p.side.text, "--side-muted": p.side.muted, "--side-faint": p.side.faint, "--side-accent": p.side.accent,
    "--bar-bg": p.bar?.bg ?? p.bg, "--bar-hover": p.bar?.hover ?? p.hover, "--bar-border": p.bar?.border ?? p.border,
    "--bar-text": p.bar?.text ?? p.text, "--bar-muted": p.bar?.muted ?? p.muted,
    // The message box is its own surface, a shade apart from the page so it
    // reads as a thing you type into rather than part of the transcript. A
    // dark theme lifts it; a light one is already at the top of its range,
    // so it takes a little of the window's own tint instead.
    "--composer": p.light
      ? `color-mix(in srgb, ${p.bg} 24%, ${p.raised})`
      : `color-mix(in srgb, ${p.text} 7%, ${p.raised})`,
    "--composer-border": `color-mix(in srgb, ${p.text} 15%, ${p.border})`,
    "--shadow": p.light ? "rgba(40, 44, 60, 0.16)" : "rgba(0, 0, 0, 0.45)",
    ...(p.extra ?? {}),
  };
}

let applied: string[] = [];

export function applyTheme(id: ThemeId) {
  const root = document.documentElement;
  const p = PALETTES[id] ?? PALETTES.graphite;
  for (const k of applied) root.style.removeProperty(k);
  const vars = themeVars(p);
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v);
  applied = Object.keys(vars);
  root.setAttribute("data-theme", id);
  // light themes need their surfaces drawn apart (borders, raised panels)
  // where dark ones separate by shade alone
  if (p.light) root.setAttribute("data-light", "");
  else root.removeAttribute("data-light");
  root.style.colorScheme = p.light ? "light" : "dark";
  localStorage.setItem("aether.theme", id);
  window.dispatchEvent(new CustomEvent("aether:prefs"));
}

export function applyLang(id: string) {
  document.documentElement.lang = id;
  document.documentElement.dir = id === "ar" ? "rtl" : "ltr";
  localStorage.setItem("aether.lang", id);
  window.dispatchEvent(new CustomEvent("aether:prefs"));
}

export function isLightTheme(id: ThemeId): boolean {
  return Boolean(PALETTES[id]?.light);
}

export function bootPrefs() {
  const saved = localStorage.getItem("aether.theme") || "graphite";
  const theme = (RETIRED[saved] ?? saved) as ThemeId;
  localStorage.setItem("aether.theme.v2", "1");
  const lang = localStorage.getItem("aether.lang") || "tr";
  applyTheme(theme in PALETTES ? theme : "graphite");
  applyLang(lang);
}
