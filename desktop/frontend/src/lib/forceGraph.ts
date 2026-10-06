// Small force-directed layout, no dependencies. O(n²) repulsion is fine for
// the few hundred nodes the map renders; group gravity pulls files of one
// package together so the graph reads as clusters of neurons.

export type SimNode = {
  id: string;
  group: string;
  r: number;
  x: number;
  y: number;
  vx: number;
  vy: number;
  pinned?: boolean;
};

export type SimLink = { s: number; t: number; w: number };

export type ForceOpts = {
  repulsion: number;
  linkDistance: number;
  linkStrength: number;
  groupGravity: number;
  centerGravity: number;
  decay: number;
};

export const DEFAULT_FORCES: ForceOpts = {
  repulsion: 520,
  linkDistance: 46,
  linkStrength: 0.05,
  groupGravity: 0.035,
  centerGravity: 0.012,
  decay: 0.55,
};

// Stable 32-bit string hash (FNV-1a) — seeds positions so a reload lays the
// graph out the same way.
export function hash(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

// seedPositions places each group on a ring and its nodes around the group
// center. Nodes that already have a position (from a previous layout) keep it.
export function seedPositions(nodes: SimNode[], keep?: Map<string, { x: number; y: number }>): void {
  const groups = [...new Set(nodes.map((n) => n.group))].sort();
  const ring = 60 + groups.length * 14;
  const centers = new Map<string, { x: number; y: number }>();
  groups.forEach((g, i) => {
    const a = (i / Math.max(1, groups.length)) * Math.PI * 2;
    centers.set(g, { x: Math.cos(a) * ring, y: Math.sin(a) * ring });
  });
  for (const n of nodes) {
    const prev = keep?.get(n.id);
    if (prev) {
      n.x = prev.x;
      n.y = prev.y;
      continue;
    }
    const c = centers.get(n.group) ?? { x: 0, y: 0 };
    const h = hash(n.id);
    const a = ((h & 0xffff) / 0xffff) * Math.PI * 2;
    const d = 20 + ((h >>> 16) / 0xffff) * 60;
    n.x = c.x + Math.cos(a) * d;
    n.y = c.y + Math.sin(a) * d;
    n.vx = 0;
    n.vy = 0;
  }
}

// tick advances the simulation one step. alpha (0..1) scales every force and
// is expected to decay toward 0 so the layout settles.
export function tick(nodes: SimNode[], links: SimLink[], alpha: number, o: ForceOpts = DEFAULT_FORCES): void {
  const n = nodes.length;
  // repulsion
  for (let i = 0; i < n; i++) {
    const a = nodes[i];
    for (let j = i + 1; j < n; j++) {
      const b = nodes[j];
      let dx = a.x - b.x;
      let dy = a.y - b.y;
      let d2 = dx * dx + dy * dy;
      if (d2 > 90000) continue; // 300px cutoff
      if (d2 < 1) {
        dx = (hash(a.id) % 7) - 3 || 1;
        dy = (hash(b.id) % 7) - 3 || 1;
        d2 = dx * dx + dy * dy;
      }
      const min = a.r + b.r + 6;
      const f = (o.repulsion * alpha) / d2 + (d2 < min * min ? 0.5 * alpha : 0);
      const d = Math.sqrt(d2);
      const fx = (dx / d) * f;
      const fy = (dy / d) * f;
      a.vx += fx;
      a.vy += fy;
      b.vx -= fx;
      b.vy -= fy;
    }
  }
  // springs
  for (const l of links) {
    const a = nodes[l.s];
    const b = nodes[l.t];
    if (!a || !b) continue;
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const d = Math.sqrt(dx * dx + dy * dy) || 1;
    const k = ((d - o.linkDistance) / d) * o.linkStrength * l.w * alpha;
    a.vx += dx * k;
    a.vy += dy * k;
    b.vx -= dx * k;
    b.vy -= dy * k;
  }
  // group + center gravity; nodes without links get extra pull so they orbit
  // the graph instead of drifting to the edge of the canvas
  const degree = new Uint16Array(n);
  for (const l of links) {
    degree[l.s]++;
    degree[l.t]++;
  }
  const cent = new Map<string, { x: number; y: number; c: number }>();
  for (const p of nodes) {
    const g = cent.get(p.group) ?? { x: 0, y: 0, c: 0 };
    g.x += p.x;
    g.y += p.y;
    g.c++;
    cent.set(p.group, g);
  }
  for (let i = 0; i < n; i++) {
    const p = nodes[i];
    const g = cent.get(p.group)!;
    const cg = o.centerGravity * (degree[i] === 0 ? 3 : 1);
    p.vx += (g.x / g.c - p.x) * o.groupGravity * alpha;
    p.vy += (g.y / g.c - p.y) * o.groupGravity * alpha;
    p.vx -= p.x * cg * alpha;
    p.vy -= p.y * cg * alpha;
  }
  for (const p of nodes) {
    if (p.pinned) {
      p.vx = 0;
      p.vy = 0;
      continue;
    }
    p.vx *= o.decay;
    p.vy *= o.decay;
    p.x += p.vx;
    p.y += p.vy;
  }
}

// bounds returns the bounding box of all nodes, padded by their radius.
export function bounds(nodes: SimNode[]): { x0: number; y0: number; x1: number; y1: number } {
  if (nodes.length === 0) return { x0: -100, y0: -100, x1: 100, y1: 100 };
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const n of nodes) {
    x0 = Math.min(x0, n.x - n.r);
    y0 = Math.min(y0, n.y - n.r);
    x1 = Math.max(x1, n.x + n.r);
    y1 = Math.max(y1, n.y + n.r);
  }
  return { x0, y0, x1, y1 };
}

// A qualitative palette that reads on dark and light backgrounds. Groups get
// colors by size so the biggest packages get the most distinct hues.
const GROUP_COLORS = [
  "#7aa2ff", "#b18cff", "#3dd6a0", "#ffb454", "#ff7a93",
  "#4cc9f0", "#e6d85c", "#f28cd4", "#8bd450", "#ff9966",
];

export function groupColors(groups: string[]): Map<string, string> {
  const count = new Map<string, number>();
  for (const g of groups) count.set(g, (count.get(g) ?? 0) + 1);
  const order = [...count.keys()].sort((a, b) => (count.get(b)! - count.get(a)!) || a.localeCompare(b));
  const out = new Map<string, string>();
  order.forEach((g, i) => out.set(g, GROUP_COLORS[i % GROUP_COLORS.length]));
  return out;
}
