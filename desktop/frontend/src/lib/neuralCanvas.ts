// Canvas renderer for the maps, drawn like a graph view: gray circles on thin
// straight links, bigger the more a node is a hub; the node you point at or
// pick and its neighbors take their colors. A file an agent just wrote sends
// a few impulses out; otherwise it is still.
// Framework-free: the React view owns state and calls into this.

import { DEFAULT_FORCES, bounds, type ForceOpts, groupColors, hash, seedPositions, tick, type SimLink, type SimNode } from "./forceGraph";

type NeuralNode = {
  id: string;
  label: string;
  group: string;
  rank: number;
  loc: number;
  in: number;
  out: number;
  // 2 = main hub, 1 = mid hub, 0 = plain: a hub is drawn larger. Left out,
  // it follows from how many links the node has
  tier?: 0 | 1 | 2;
};

type Shape = 0 | 1 | 2;

type NeuralEdge = { from: string; to: string; kind: string };

type Pulse = { li: number; t: number; speed: number; dir: 1 | -1; bright: boolean };

type Theme = { bg: string; text: string; muted: string; accent: string };

type NeuralHandlers = {
  onSelect?: (id: string | null) => void;
  onHover?: (id: string | null) => void;
};

const MAX_PULSES = 320;

export class NeuralCanvas {
  private canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private nodes: SimNode[] = [];
  private meta = new Map<string, NeuralNode>();
  private index = new Map<string, number>();
  private links: (SimLink & { kind: string; bend: number })[] = [];
  private shapes: Shape[] = [];
  private forces: ForceOpts = DEFAULT_FORCES;
  private adj: number[][] = [];
  private colors = new Map<string, string>();
  private pulses: Pulse[] = [];
  private flashes = new Map<number, number>();
  private cam = { x: 0, y: 0, k: 1 };
  // a camera move under way: eased from one view to another
  // the zoom of the last full view (fit): camera moves are measured from it
  private homeK = 1;
  private tween: { from: { x: number; y: number; k: number }; to: { x: number; y: number; k: number }; t0: number; ms: number } | null = null;
  private alpha = 1;
  private raf = 0;
  private w = 0;
  private h = 0;
  private dpr = 1;
  private theme: Theme = { bg: "#08090b", text: "#f2f4f7", muted: "#9aa3b2", accent: "#8eb0ff" };
  private selected = -1;
  private hover = -1;
  private marked = new Set<number>();
  private markColor = "#ffb454";
  private highlight = new Set<number>();
  private drag: { mode: "pan" | "node"; i: number; sx: number; sy: number; moved: boolean } | null = null;
  private handlers: NeuralHandlers;
  private ro: ResizeObserver | null = null;
  private fitted = false;

  constructor(canvas: HTMLCanvasElement, handlers: NeuralHandlers = {}) {
    this.canvas = canvas;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("canvas 2d unavailable");
    this.ctx = ctx;
    this.handlers = handlers;
    this.readTheme();
    this.resize();
    if (typeof ResizeObserver !== "undefined") {
      this.ro = new ResizeObserver(() => this.resize());
      this.ro.observe(canvas);
    }
    canvas.addEventListener("pointerdown", this.onDown);
    canvas.addEventListener("pointermove", this.onMove);
    canvas.addEventListener("pointerup", this.onUp);
    canvas.addEventListener("pointerleave", this.onLeave);
    canvas.addEventListener("wheel", this.onWheel, { passive: false });
    canvas.addEventListener("dblclick", this.onDbl);
    this.loop();
  }

  destroy(): void {
    cancelAnimationFrame(this.raf);
    this.ro?.disconnect();
    const c = this.canvas;
    c.removeEventListener("pointerdown", this.onDown);
    c.removeEventListener("pointermove", this.onMove);
    c.removeEventListener("pointerup", this.onUp);
    c.removeEventListener("pointerleave", this.onLeave);
    c.removeEventListener("wheel", this.onWheel);
    c.removeEventListener("dblclick", this.onDbl);
  }

  readTheme(): void {
    const cs = getComputedStyle(document.documentElement);
    const v = (name: string, fb: string) => cs.getPropertyValue(name).trim() || fb;
    this.theme = {
      bg: v("--bg-sunken", "#08090b"),
      text: v("--text", "#f2f4f7"),
      muted: v("--muted", "#9aa3b2"),
      accent: v("--accent", "#8eb0ff"),
    };
  }

  // setData replaces the graph. Positions of nodes that survive are kept, so
  // a refresh after an edit does not reshuffle the map.
  // colors, when given, fixes a color per group instead of assigning them
  // by group size (the context map keeps "file" one color in every chat).
  setData(nodes: NeuralNode[], edges: NeuralEdge[], colors?: Map<string, string>): void {
    const keep = new Map<string, { x: number; y: number }>();
    for (const n of this.nodes) keep.set(n.id, { x: n.x, y: n.y });
    const maxRank = Math.max(1e-9, ...nodes.map((n) => n.rank));
    const sim: SimNode[] = nodes.map((n) => ({
      id: n.id,
      group: n.group,
      r: 3.5 + Math.sqrt(n.rank / maxRank) * 11 + Math.min(3, Math.log10(1 + n.loc) * 0.8),
      x: 0, y: 0, vx: 0, vy: 0,
    }));
    const fresh = sim.filter((n) => !keep.has(n.id)).length;
    seedPositions(sim, keep);
    for (const n of sim) {
      const prev = this.nodes[this.index.get(n.id) ?? -1];
      if (prev?.pinned) n.pinned = true;
    }
    this.nodes = sim;
    this.meta = new Map(nodes.map((n) => [n.id, n]));
    this.index = new Map(sim.map((n, i) => [n.id, i]));
    this.colors = colors ?? groupColors(nodes.map((n) => n.group));
    this.links = [];
    this.adj = sim.map(() => []);
    for (const e of edges) {
      const s = this.index.get(e.from);
      const t = this.index.get(e.to);
      if (s === undefined || t === undefined || s === t) continue;
      const bend = ((hash(e.from + ">" + e.to) % 100) / 100 - 0.5) * 0.5;
      this.links.push({ s, t, w: e.kind === "call" ? 0.5 : 1, kind: e.kind, bend });
      this.adj[s].push(this.links.length - 1);
      this.adj[t].push(this.links.length - 1);
    }
    // Hubs by how connected they are, like a graph view: the few most
    // linked nodes, then the next band, are drawn larger than the rest.
    const deg = this.adj.map((a) => a.length);
    const sorted = [...deg].sort((a, b) => b - a);
    const hubAt = Math.max(3, sorted[Math.max(0, Math.ceil(sim.length * 0.04) - 1)] ?? 0);
    const midAt = Math.max(2, sorted[Math.max(0, Math.ceil(sim.length * 0.2) - 1)] ?? 0);
    this.shapes = sim.map((n, i) => {
      const fixed = this.meta.get(n.id)?.tier;
      if (fixed !== undefined) return fixed;
      return deg[i] >= hubAt ? 2 : deg[i] >= midAt ? 1 : 0;
    });
    // a hub reads as one: it is drawn a little larger
    sim.forEach((n, i) => { n.r *= this.shapes[i] === 2 ? 1.35 : this.shapes[i] === 1 ? 1.1 : 0.9; });
    this.pulses = [];
    this.selected = -1;
    this.hover = -1;
    this.marked.clear();
    this.highlight.clear();
    if (fresh > 0 || !this.fitted) {
      this.alpha = 1;
      // settle most of the layout before the first frame
      const pre = sim.length > 500 ? 60 : 120;
      for (let i = 0; i < pre; i++) {
        tick(this.nodes, this.links, this.alpha, this.forces);
        this.alpha *= 0.975;
      }
    }
    if (!this.fitted && sim.length > 0) {
      this.fit();
      this.fitted = true;
    }
  }

  colorOf(group: string): string {
    return this.colors.get(group) ?? this.theme.accent;
  }

  groups(): { group: string; color: string; count: number }[] {
    const count = new Map<string, number>();
    for (const n of this.nodes) count.set(n.group, (count.get(n.group) ?? 0) + 1);
    return [...count.entries()]
      .sort((a, b) => b[1] - a[1])
      .map(([group, c]) => ({ group, color: this.colorOf(group), count: c }));
  }

  select(id: string | null, focus = false): void {
    this.selected = id ? this.index.get(id) ?? -1 : -1;
    if (focus && this.selected >= 0) {
      const n = this.nodes[this.selected];
      this.cam.x = -n.x * this.cam.k;
      this.cam.y = -n.y * this.cam.k;
    }
  }

  // mark tints a set of files (search hits, impact set) without selecting.
  mark(ids: string[], color?: string): void {
    this.marked = new Set(ids.map((id) => this.index.get(id)).filter((i): i is number => i !== undefined));
    if (color) this.markColor = color;
  }

  setHighlight(ids: string[]): void {
    this.highlight = new Set(ids.map((id) => this.index.get(id)).filter((i): i is number => i !== undefined));
  }

  // fire makes a file light up and send impulses to its neighbors — used when
  // an agent writes the file.
  fire(id: string): boolean {
    const i = this.index.get(id);
    if (i === undefined) return false;
    this.flashes.set(i, performance.now());
    for (const li of this.adj[i]) {
      const l = this.links[li];
      this.pushPulse({ li, t: 0, speed: 0.012 + Math.random() * 0.01, dir: l.s === i ? 1 : -1, bright: true });
    }
    return true;
  }

  // focusOn glides the view to a set of nodes, framing them with room
  // around, and never closer than maxK: it shows where the talk is, it does
  // not dive into it. Those nodes light up in their colors.
  focusOn(ids: string[], opts: { minZoom?: number; maxZoom?: number; pad?: number; shiftX?: number } = {}): void {
    const idx = ids.map((id) => this.index.get(id)).filter((i): i is number => i !== undefined);
    this.highlight = new Set(idx);
    if (idx.length === 0) return;
    const pts = idx.map((i) => this.nodes[i]);
    const x0 = Math.min(...pts.map((n) => n.x - n.r)), x1 = Math.max(...pts.map((n) => n.x + n.r));
    const y0 = Math.min(...pts.map((n) => n.y - n.r)), y1 = Math.max(...pts.map((n) => n.y + n.r));
    const pad = opts.pad ?? 140;
    const fitK = Math.min((this.w - pad * 2) / Math.max(1, x1 - x0), (this.h - pad * 2) / Math.max(1, y1 - y0));
    // Zoom is measured from the full view: it moves in toward the talk (at
    // least minZoom times closer) but not into it (at most maxZoom). When the
    // nodes named are far apart, it frames where most of them are.
    const k = Math.max(this.homeK * (opts.minZoom ?? 1.15), Math.min(this.homeK * (opts.maxZoom ?? 1.8), fitK));
    const cx = pts.reduce((a, n) => a + n.x, 0) / pts.length;
    const cy = pts.reduce((a, n) => a + n.y, 0) / pts.length;
    const mid = fitK >= k ? { x: (x0 + x1) / 2, y: (y0 + y1) / 2 } : { x: cx, y: cy };
    const to = { k, x: -mid.x * k + (opts.shiftX ?? 0), y: -mid.y * k };
    this.tween = { from: { ...this.cam }, to, t0: performance.now(), ms: 700 };
  }

  clearHighlight(): void {
    this.highlight = new Set();
  }

  fit(): void {
    const b = bounds(this.nodes);
    const bw = Math.max(1, b.x1 - b.x0);
    const bh = Math.max(1, b.y1 - b.y0);
    const k = Math.min(2, Math.max(0.15, Math.min((this.w - 60) / bw, (this.h - 60) / bh)));
    this.homeK = k;
    this.cam.k = k;
    this.cam.x = -((b.x0 + b.x1) / 2) * k;
    this.cam.y = -((b.y0 + b.y1) / 2) * k;
  }

  // setForces loosens or tightens the layout; a small graph reads better
  // spread out than packed like a big one.
  setForces(f: Partial<ForceOpts>): void {
    this.forces = { ...DEFAULT_FORCES, ...f };
    this.reheat(0.8);
  }

  reheat(a = 0.5): void {
    this.alpha = Math.max(this.alpha, a);
  }

  private resize = (): void => {
    const r = this.canvas.getBoundingClientRect();
    this.dpr = Math.min(2, window.devicePixelRatio || 1);
    this.w = Math.max(1, r.width);
    this.h = Math.max(1, r.height);
    this.canvas.width = Math.round(this.w * this.dpr);
    this.canvas.height = Math.round(this.h * this.dpr);
  };

  private pushPulse(p: Pulse): void {
    if (this.pulses.length >= MAX_PULSES) this.pulses.shift();
    this.pulses.push(p);
  }

  private neighbors(i: number): Set<number> {
    const out = new Set<number>();
    for (const li of this.adj[i] ?? []) {
      const l = this.links[li];
      out.add(l.s === i ? l.t : l.s);
    }
    return out;
  }

  private loop = (): void => {
    this.raf = requestAnimationFrame(this.loop);
    if (this.tween) {
      const { from, to, t0, ms } = this.tween;
      const u = Math.min(1, (performance.now() - t0) / ms);
      const e = 1 - Math.pow(1 - u, 3);
      this.cam.x = from.x + (to.x - from.x) * e;
      this.cam.y = from.y + (to.y - from.y) * e;
      this.cam.k = from.k + (to.k - from.k) * e;
      if (u >= 1) this.tween = null;
    }
    if (this.alpha > 0.004) {
      tick(this.nodes, this.links, this.alpha, DEFAULT_FORCES);
      this.alpha *= 0.985;
    }
    this.spawn();
    this.draw();
  };

  private spawn(): void {
    // Calm by default, as in a graph view: nothing drifts along the links.
    // Only a file an agent just wrote sends impulses out (fire()).
    if (this.pulses.length === 0) return;
    for (const p of this.pulses) p.t += p.speed;
    this.pulses = this.pulses.filter((p) => p.t < 1);
  }

  private draw(): void {
    const { ctx, dpr, w, h, cam, theme } = this;
    const now = performance.now();
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = theme.bg;
    ctx.fillRect(0, 0, w, h);

    ctx.setTransform(dpr * cam.k, 0, 0, dpr * cam.k, dpr * (w / 2 + cam.x), dpr * (h / 2 + cam.y));
    const focus = this.selected >= 0 ? this.selected : this.hover;
    const near = focus >= 0 ? this.neighbors(focus) : null;
    // hubs have dozens of neighbors; label only the most important ones
    const labelled = near && near.size > 24
      ? new Set([...near].sort((a, b) => this.nodes[b].r - this.nodes[a].r).slice(0, 24))
      : near;
    const dimming = focus >= 0 || this.marked.size > 0 || this.highlight.size > 0;

    // links: thin, straight, neutral; the focus's own light up
    ctx.lineCap = "round";
    const hair = 1 / cam.k;
    for (const l of this.links) {
      const a = this.nodes[l.s];
      const b = this.nodes[l.t];
      const on = focus >= 0 && (l.s === focus || l.t === focus);
      const hl = this.highlight.has(l.s) && this.highlight.has(l.t);
      let alpha = l.kind === "call" ? 0.16 : 0.24;
      if (on || hl) alpha = 0.75;
      else if (dimming) alpha *= 0.3;
      ctx.strokeStyle = withAlpha(on || hl ? theme.accent : theme.muted, alpha);
      ctx.lineWidth = (on || hl ? 1.4 : 0.9) * hair;
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.lineTo(b.x, b.y);
      ctx.stroke();
    }

    // impulses from fire(): small solid dots
    for (const p of this.pulses) {
      const l = this.links[p.li];
      const a = this.nodes[l.s];
      const b = this.nodes[l.t];
      const t = p.dir === 1 ? p.t : 1 - p.t;
      ctx.globalAlpha = Math.sin(Math.PI * p.t);
      ctx.fillStyle = theme.accent;
      ctx.beginPath();
      ctx.arc(a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t, 2.4 * hair * Math.sqrt(cam.k), 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;

    // nodes: flat gray circles, as in a graph view; only the one you point
    // at or pick, and the nodes it links to, take their colors
    const gray = theme.muted;
    for (let i = 0; i < this.nodes.length; i++) {
      const n = this.nodes[i];
      const isFocus = i === focus;
      const isNear = near?.has(i) ?? false;
      const isMarked = this.marked.has(i) || this.highlight.has(i);
      const faded = dimming && !isFocus && !isNear && !isMarked;
      const flashAt = this.flashes.get(i);
      const flash = flashAt ? Math.max(0, 1 - (now - flashAt) / 1600) : 0;
      if (flashAt && flash === 0) this.flashes.delete(i);

      // what the assistant is talking about lights up like a pick
      const lit = isFocus || isNear || this.highlight.has(i);
      ctx.globalAlpha = faded ? 0.25 : lit || isMarked ? 1 : 0.75;
      ctx.fillStyle = isMarked && !lit ? this.markColor : lit ? this.colorOf(n.group) : gray;
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.r, 0, Math.PI * 2);
      ctx.fill();
      if (isFocus || flash > 0) {
        ctx.globalAlpha = 1;
        ctx.strokeStyle = isFocus ? theme.text : theme.accent;
        ctx.lineWidth = 1.4 * hair;
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.r + 3 * hair + flash * 10, 0, Math.PI * 2);
        ctx.stroke();
      }
    }
    ctx.globalAlpha = 1;

    // labels
    const fs = 11 / cam.k;
    ctx.font = `${fs}px ${getComputedStyle(this.canvas).fontFamily || "sans-serif"}`;
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    // When many nodes want a label (a wide impact, a hub) they pile into an
    // unreadable heap. The focus first, then the marked, then by size: each
    // label takes its box only if it does not overlap one already drawn.
    const want: number[] = [];
    for (let i = 0; i < this.nodes.length; i++) {
      const n = this.nodes[i];
      const isFocus = i === focus;
      // like a graph view: names come with zoom, hover and the main hubs
      const show = isFocus || labelled?.has(i) || this.marked.has(i) || this.highlight.has(i)
        || (!dimming && (this.shapes[i] === 2 || (cam.k >= 1.3 && n.r * cam.k > 7)));
      if (!show) continue;
      if (dimming && !isFocus && !near?.has(i) && !this.marked.has(i) && !this.highlight.has(i)) continue;
      want.push(i);
    }
    const rank = (i: number) => (i === focus ? 3 : this.marked.has(i) ? 2 : 0) * 1e6 + this.nodes[i].r;
    want.sort((a, b) => rank(b) - rank(a));
    const taken: [number, number, number, number][] = [];
    for (const i of want) {
      const n = this.nodes[i];
      const isFocus = i === focus;
      const label = this.meta.get(n.id)?.label ?? n.id;
      const tw = ctx.measureText(label).width;
      const box: [number, number, number, number] = [n.x - tw / 2 - 3 / cam.k, n.y + n.r + 3 / cam.k, tw + 6 / cam.k, fs + 4 / cam.k];
      if (!isFocus && taken.some(([x, y, bw, bh]) => box[0] < x + bw && x < box[0] + box[2] && box[1] < y + bh && y < box[1] + box[3])) continue;
      taken.push(box);
      ctx.fillStyle = isFocus ? theme.text : withAlpha(theme.muted, 0.95);
      ctx.fillText(label, n.x, n.y + n.r + 5 / cam.k);
    }
  }

  // ── input ────────────────────────────────────────────────────────────────

  private toWorld(sx: number, sy: number): { x: number; y: number } {
    return { x: (sx - this.w / 2 - this.cam.x) / this.cam.k, y: (sy - this.h / 2 - this.cam.y) / this.cam.k };
  }

  private pick(sx: number, sy: number): number {
    const p = this.toWorld(sx, sy);
    let best = -1;
    let bestD = Infinity;
    for (let i = 0; i < this.nodes.length; i++) {
      const n = this.nodes[i];
      const d = Math.hypot(n.x - p.x, n.y - p.y);
      const hit = n.r + 4 / this.cam.k;
      if (d < hit && d < bestD) {
        best = i;
        bestD = d;
      }
    }
    return best;
  }

  private local(e: PointerEvent | WheelEvent | MouseEvent): { x: number; y: number } {
    const r = this.canvas.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  private onDown = (e: PointerEvent): void => {
    const p = this.local(e);
    const i = this.pick(p.x, p.y);
    try {
      this.canvas.setPointerCapture(e.pointerId);
    } catch {
      /* synthetic or already-released pointer */
    }
    this.tween = null; // a hand on the map wins over a camera move
    this.drag = { mode: i >= 0 ? "node" : "pan", i, sx: p.x, sy: p.y, moved: false };
  };

  private onMove = (e: PointerEvent): void => {
    const p = this.local(e);
    if (!this.drag) {
      const i = this.pick(p.x, p.y);
      if (i !== this.hover) {
        this.hover = i;
        this.canvas.style.cursor = i >= 0 ? "pointer" : "grab";
        this.handlers.onHover?.(i >= 0 ? this.nodes[i].id : null);
      }
      return;
    }
    const dx = p.x - this.drag.sx;
    const dy = p.y - this.drag.sy;
    if (Math.abs(dx) + Math.abs(dy) > 3) this.drag.moved = true;
    if (this.drag.mode === "pan") {
      this.cam.x += dx;
      this.cam.y += dy;
      this.drag.sx = p.x;
      this.drag.sy = p.y;
      this.canvas.style.cursor = "grabbing";
    } else if (this.drag.moved) {
      const n = this.nodes[this.drag.i];
      const w = this.toWorld(p.x, p.y);
      n.x = w.x;
      n.y = w.y;
      n.pinned = true;
      this.reheat(0.25);
    }
  };

  private onUp = (e: PointerEvent): void => {
    try {
      this.canvas.releasePointerCapture(e.pointerId);
    } catch {
      /* not captured */
    }
    const d = this.drag;
    this.drag = null;
    if (!d || d.moved) return;
    const id = d.i >= 0 ? this.nodes[d.i].id : null;
    this.selected = d.i;
    this.handlers.onSelect?.(id);
  };

  private onLeave = (): void => {
    if (this.hover >= 0) {
      this.hover = -1;
      this.handlers.onHover?.(null);
    }
  };

  private onWheel = (e: WheelEvent): void => {
    this.tween = null;
    e.preventDefault();
    const p = this.local(e);
    const before = this.toWorld(p.x, p.y);
    const k = Math.min(6, Math.max(0.08, this.cam.k * Math.exp(-e.deltaY * 0.0015)));
    this.cam.k = k;
    this.cam.x = p.x - this.w / 2 - before.x * k;
    this.cam.y = p.y - this.h / 2 - before.y * k;
  };

  private onDbl = (e: MouseEvent): void => {
    const p = this.local(e);
    const i = this.pick(p.x, p.y);
    if (i < 0) {
      this.fit();
      return;
    }
    const n = this.nodes[i];
    n.pinned = false;
    this.cam.k = Math.max(this.cam.k, 1.4);
    this.cam.x = -n.x * this.cam.k;
    this.cam.y = -n.y * this.cam.k;
  };
}

// withAlpha turns #rgb/#rrggbb (or any CSS color) into an rgba() string.
export function withAlpha(color: string, a: number): string {
  const c = color.trim();
  if (c.startsWith("#")) {
    let hex = c.slice(1);
    if (hex.length === 3) hex = hex.split("").map((ch) => ch + ch).join("");
    const n = parseInt(hex.slice(0, 6), 16);
    if (!Number.isNaN(n)) return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
  }
  const m = c.match(/^rgba?\(([^)]+)\)$/);
  if (m) {
    const [r, g, b] = m[1].split(",").map((x) => x.trim());
    return `rgba(${r},${g},${b},${a})`;
  }
  return c;
}
