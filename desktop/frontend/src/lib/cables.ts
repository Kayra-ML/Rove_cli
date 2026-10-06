// Geometry for the context map: session cards joined by straight cables.
// All coordinates are world coordinates (before camera pan/zoom).

export const CARD_W = 232;
export const CARD_H = 92;

export type Pt = { x: number; y: number };
export type Rect = { x: number; y: number; w: number; h: number };

export function cardRect(p: Pt): Rect {
  return { x: p.x, y: p.y, w: CARD_W, h: CARD_H };
}

// anchors joins two cards corner to corner: of the four corners of each,
// the two closest to one another, joined by a straight cable. A cable then
// never leaves from the middle of an edge. fromSide/toSide say which way
// each end faces horizontally.
export function anchors(a: Rect, b: Rect): { from: Pt; to: Pt; fromSide: 1 | -1; toSide: 1 | -1 } {
  let best = { from: corners(a)[0], to: corners(b)[0], d: Infinity };
  for (const p of corners(a)) {
    for (const q of corners(b)) {
      const d = Math.hypot(p.x - q.x, p.y - q.y);
      // ties (side by side, level) go to the upper pair
      if (d < best.d - 0.5 || (Math.abs(d - best.d) <= 0.5 && p.y + q.y < best.from.y + best.to.y)) best = { from: p, to: q, d };
    }
  }
  const right: 1 | -1 = b.x + b.w / 2 >= a.x + a.w / 2 ? 1 : -1;
  return { from: best.from, to: best.to, fromSide: right, toSide: right === 1 ? -1 : 1 };
}

function corners(r: Rect): Pt[] {
  return [
    { x: r.x, y: r.y }, { x: r.x + r.w, y: r.y },
    { x: r.x, y: r.y + r.h }, { x: r.x + r.w, y: r.y + r.h },
  ];
}

// spread moves cable ends that leave a card at the same point apart along
// that card's edge, so several cables from one side do not all come out of
// one spot. ends are [card id, point]; it returns the points in order.
export function spread(ends: [string, Pt][], rects: Map<string, Rect>, gap = 14): Pt[] {
  const out = ends.map(([, p]) => ({ ...p }));
  const groups = new Map<string, number[]>();
  ends.forEach(([id, p], i) => {
    const k = `${id}@${Math.round(p.x / 4)},${Math.round(p.y / 4)}`;
    groups.set(k, [...(groups.get(k) ?? []), i]);
  });
  for (const idx of groups.values()) {
    if (idx.length < 2) continue;
    const [id, p] = ends[idx[0]];
    const rc = rects.get(id);
    if (!rc) continue;
    // on a left or right edge the ends spread up and down, else sideways
    const upright = Math.abs(p.x - rc.x) < 1 || Math.abs(p.x - (rc.x + rc.w)) < 1;
    idx.forEach((i, n) => {
      const d = (n - (idx.length - 1) / 2) * gap;
      if (upright) out[i].y = clamp(p.y + d, rc.y + 6, rc.y + rc.h - 6);
      else out[i].x = clamp(p.x + d, rc.x + 6, rc.x + rc.w - 6);
    });
  }
  return out;
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, v));
}

// overlaps reports whether two cards come closer than gap.
export function overlaps(a: Rect, b: Rect, gap = 12): boolean {
  return a.x < b.x + b.w + gap && b.x < a.x + a.w + gap && a.y < b.y + b.h + gap && b.y < a.y + a.h + gap;
}

// slide is where a dragged card may go: where it is pointed at if that is
// clear of every other card, else as far along one axis as stays clear,
// else where it was. Cards never go into one another.
export function slide(prev: Pt, next: Pt, others: Rect[]): Pt {
  const clear = (p: Pt) => !others.some((o) => overlaps(cardRect(p), o));
  // a card already sitting on another (laid out before this rule) can be
  // pulled off it freely
  if (clear(next) || !clear(prev)) return next;
  const xOnly = { x: next.x, y: prev.y };
  if (clear(xOnly)) return xOnly;
  const yOnly = { x: prev.x, y: next.y };
  if (clear(yOnly)) return yOnly;
  return prev;
}

// edge is where the ray from a card's centre c towards p crosses its border.
export function edge(rc: Rect, c: Pt, p: Pt): Pt {
  const dx = p.x - c.x;
  const dy = p.y - c.y;
  if (dx === 0 && dy === 0) return c;
  const tx = dx === 0 ? Infinity : rc.w / 2 / Math.abs(dx);
  const ty = dy === 0 ? Infinity : rc.h / 2 / Math.abs(dy);
  const k = Math.min(tx, ty);
  return { x: c.x + dx * k, y: c.y + dy * k };
}

type Cable = { d: string; c1: Pt; c2: Pt; from: Pt; to: Pt };

// cable is a straight line between two points. It keeps the shape of a
// cubic (its control points at the thirds) so a point along it — the
// arrow at its middle, a pulse moving along it — is found the same way.
// The sides are kept for callers; a straight cable does not need them.
export function cable(from: Pt, to: Pt, _fromSide: 1 | -1 = 1, _toSide: 1 | -1 = -1): Cable {
  const c1 = { x: from.x + (to.x - from.x) / 3, y: from.y + (to.y - from.y) / 3 };
  const c2 = { x: from.x + ((to.x - from.x) * 2) / 3, y: from.y + ((to.y - from.y) * 2) / 3 };
  const d = `M ${r(from.x)} ${r(from.y)} L ${r(to.x)} ${r(to.y)}`;
  return { d, c1, c2, from, to };
}

// at finds the point at t along the cable and the direction it runs, in
// degrees.
export function at(c: Cable, t: number): { x: number; y: number; angle: number } {
  const u = 1 - t;
  const x = u * u * u * c.from.x + 3 * u * u * t * c.c1.x + 3 * u * t * t * c.c2.x + t * t * t * c.to.x;
  const y = u * u * u * c.from.y + 3 * u * u * t * c.c1.y + 3 * u * t * t * c.c2.y + t * t * t * c.to.y;
  const tx = 3 * u * u * (c.c1.x - c.from.x) + 6 * u * t * (c.c2.x - c.c1.x) + 3 * t * t * (c.to.x - c.c2.x);
  const ty = 3 * u * u * (c.c1.y - c.from.y) + 6 * u * t * (c.c2.y - c.c1.y) + 3 * t * t * (c.to.y - c.c2.y);
  return { x, y, angle: (Math.atan2(ty, tx) * 180) / Math.PI };
}

export function hitCard(p: Pt, rects: Map<string, Rect>, except?: string): string | null {
  for (const [id, rc] of rects) {
    if (id === except) continue;
    if (p.x >= rc.x && p.x <= rc.x + rc.w && p.y >= rc.y && p.y <= rc.y + rc.h) return id;
  }
  return null;
}

// fitView returns a camera that frames every card with padding.
export function fitView(rects: Rect[], w: number, h: number): { x: number; y: number; k: number } {
  if (rects.length === 0) return { x: w / 2 - CARD_W / 2, y: h / 2 - CARD_H / 2, k: 1 };
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const rc of rects) {
    x0 = Math.min(x0, rc.x);
    y0 = Math.min(y0, rc.y);
    x1 = Math.max(x1, rc.x + rc.w);
    y1 = Math.max(y1, rc.y + rc.h);
  }
  const pad = 80;
  const k = Math.max(0.3, Math.min(1.2, Math.min((w - pad * 2) / (x1 - x0), (h - pad * 2) / (y1 - y0))));
  return { k, x: w / 2 - ((x0 + x1) / 2) * k, y: h / 2 - ((y0 + y1) / 2) * k };
}

// Where a card dropped at world point p should go so it does not overlap
// another one: straight below the one in the way, with a gap to read by.
const GAP = 24;
export function freeSpot(p: Pt, rects: Rect[]): Pt {
  let q = { ...p };
  for (let i = 0; i < 20; i++) {
    const hit = rects.find((rc) => Math.abs(rc.x - q.x) < CARD_W + GAP && Math.abs(rc.y - q.y) < CARD_H + GAP);
    if (!hit) return q;
    q = { x: q.x, y: hit.y + CARD_H + GAP };
  }
  return q;
}

function r(n: number): number {
  return Math.round(n * 10) / 10;
}
