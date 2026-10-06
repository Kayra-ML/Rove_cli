// Sidebar sizing for the shell. Widths are clamped to their own bounds and
// so that the center column never drops below MIN_MAIN.

// a hairline between the side list and the work; its grab area is wider
const GUTTER = 1;
const MIN_MAIN = 380;

// maxPct bounds a panel to a share of the window, so it never reaches a
// disproportionate size on a normal-width screen even though the absolute
// `max` (meant for very wide monitors) is larger.
export const SIDE = { def: 250, min: 180, max: 440, maxPct: 0.32 };
export const RIGHT = { def: 380, min: 260, max: 760, maxPct: 0.45 };

// fitsSide says whether the window is wide enough to hold the sidebar beside
// the work at their smallest honest sizes. Below that the shell stops giving
// the list a column of its own and shows it over the page instead, so a
// narrow window is a usable one rather than two cramped halves.
export function fitsSide(total: number): boolean {
  return total >= SIDE.min + MIN_MAIN + GUTTER * 4;
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

// maxWidth is the largest a panel may be right now: its own absolute cap,
// a fraction of the window, and whatever is left after the other panel and
// the minimum center column — whichever is smallest.
export function maxWidth(side: "left" | "right", other: number, total: number): number {
  const b = side === "left" ? SIDE : RIGHT;
  const room = total - other - MIN_MAIN - GUTTER * 4;
  return Math.max(b.min, Math.min(b.max, Math.round(total * b.maxPct), room));
}

// dragWidth is the new width of a panel dragged by dx pixels. The left panel
// grows when dragged right, the right panel when dragged left. other is the
// width taken by the opposite panel (0 when hidden); total is the window.
export function dragWidth(
  side: "left" | "right",
  start: number,
  dx: number,
  other: number,
  total: number,
): number {
  const b = side === "left" ? SIDE : RIGHT;
  const raw = side === "left" ? start + dx : start - dx;
  return Math.round(clamp(raw, b.min, maxWidth(side, other, total)));
}

export function columns(side: number | null, right: number | null): string {
  const cols: string[] = [];
  if (side != null) cols.push(`${side}px`, `${GUTTER}px`);
  cols.push("minmax(0, 1fr)");
  if (right != null) cols.push(`${GUTTER}px`, `${right}px`);
  return cols.join(" ");
}

export function loadWidth(key: string, def: number, min: number, max: number): number {
  try {
    const n = Number(localStorage.getItem(key));
    return Number.isFinite(n) && n > 0 ? clamp(n, min, max) : def;
  } catch {
    return def;
  }
}
