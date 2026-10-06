import type { WorkPhase, WorkPlan } from "./types";

// Pure helpers for the Teamwork view: how a plan reads at a glance.

type Wave = { n: number; phases: WorkPhase[] };

// wavesOf groups the work phases by wave, in order; the merge phase is
// returned apart, since it is drawn as the last column.
export function wavesOf(plan: WorkPlan): { waves: Wave[]; merge: WorkPhase | null } {
  const byWave = new Map<number, WorkPhase[]>();
  let merge: WorkPhase | null = null;
  for (const p of plan.phases) {
    if (p.merge) { merge = p; continue; }
    const list = byWave.get(p.wave) ?? [];
    list.push(p);
    byWave.set(p.wave, list);
  }
  const waves = [...byWave.entries()].sort((a, b) => a[0] - b[0]).map(([n, phases]) => ({ n, phases }));
  return { waves, merge };
}

// stats: orchestras, players (merge included), how many start together,
// and how many junctions the cables meet at.
export function statsOf(plan: WorkPlan): { phases: number; agents: number; parallel: number; junctions: number } {
  const work = plan.phases.filter((p) => !p.merge);
  const first = work.filter((p) => p.wave === 1).length;
  const into = new Map<string, number>();
  for (const [, to] of linksOf(plan)) into.set(to, (into.get(to) ?? 0) + 1);
  return {
    phases: work.length,
    agents: plan.phases.reduce((n, p) => n + p.agents.length, 0),
    parallel: first > 1 ? first : 0,
    junctions: [...into.values()].filter((n) => n >= 2).length,
  };
}

// startLabel says when a phase starts: first, alongside others, or after
// the phases it depends on.
export function startLabel(p: WorkPhase, plan: WorkPlan): { kind: "first" | "parallel" | "after"; after?: string[] } {
  if (p.dependsOn && p.dependsOn.length > 0 && !p.merge) return { kind: "after", after: p.dependsOn };
  const sameWave = plan.phases.filter((x) => !x.merge && x.wave === p.wave).length;
  return { kind: sameWave > 1 ? "parallel" : "first" };
}

// Links drawn between cards: each dependency, and every last-wave phase
// into the merge.
export function linksOf(plan: WorkPlan): [string, string][] {
  const out: [string, string][] = [];
  const { waves, merge } = wavesOf(plan);
  for (const p of plan.phases) {
    if (p.merge) continue;
    for (const d of p.dependsOn ?? []) out.push([d, p.id]);
  }
  if (merge) {
    // a phase feeds the merge unless a later phase depends on it
    const fed = new Set(out.map(([from]) => from));
    for (const w of waves) for (const p of w.phases) if (!fed.has(p.id)) out.push([p.id, merge.id]);
  }
  return out;
}


// ── the orchestra graph ──────────────────────────────────────────────────
// Each part is an orchestra card; cards stand in columns by when they can
// start (the merge last), cables run from a part to the parts that need its
// work, and where two or more cables meet a junction sits in front of the
// part they feed: the place their work comes together.

export const CARD_W = 252;
export const CARD_H = 150;
const COL_GAP = 112;
const ROW_GAP = 22;
const PAD = 24;
// room above the cards for each column's label
const HEAD = 30;

export type Rect = { x: number; y: number; w: number; h: number };
export type Junction = { id: string; to: string; from: string[]; x: number; y: number };
export type Cable = { key: string; from: string; to: string; d: string; into: "card" | "junction" | "out" };
// Column is where a column's label stands: the wave it holds, or the merge.
export type Column = { x: number; y: number; wave: number; merge: boolean };
export type Graph = { nodes: Map<string, Rect>; junctions: Junction[]; cables: Cable[]; columns: Column[]; width: number; height: number };

// columnsOf puts each part in the column of its wave, the merge after all;
// within a column parts are ordered by where the parts they hang on stand,
// so cables cross as little as they can.
function columnsOf(plan: WorkPlan): WorkPhase[][] {
  const { waves, merge } = wavesOf(plan);
  const cols = waves.map((w) => [...w.phases]);
  if (merge) cols.push([merge]);
  const row = new Map<string, number>();
  cols.forEach((col, ci) => {
    if (ci > 0) {
      const at = (p: WorkPhase) => {
        const ds = (p.dependsOn ?? []).map((d) => row.get(d)).filter((n): n is number => n !== undefined);
        return ds.length ? ds.reduce((a, b) => a + b, 0) / ds.length : Number.MAX_SAFE_INTEGER;
      };
      col.sort((a, b) => at(a) - at(b));
    }
    col.forEach((p, i) => row.set(p.id, i));
  });
  return cols;
}

export function graphOf(plan: WorkPlan): Graph {
  const cols = columnsOf(plan);
  const tallest = Math.max(1, ...cols.map((c) => c.length));
  const body = tallest * CARD_H + (tallest - 1) * ROW_GAP;
  const height = PAD * 2 + HEAD + body;
  const nodes = new Map<string, Rect>();
  const columns: Column[] = [];
  cols.forEach((col, ci) => {
    const x = PAD + ci * (CARD_W + COL_GAP);
    const colH = col.length * CARD_H + (col.length - 1) * ROW_GAP;
    const top = PAD + HEAD + (body - colH) / 2;
    columns.push({ x, y: PAD, wave: col[0]?.wave ?? ci + 1, merge: Boolean(col[0]?.merge) });
    col.forEach((p, i) => nodes.set(p.id, { x, y: top + i * (CARD_H + ROW_GAP), w: CARD_W, h: CARD_H }));
  });
  const width = PAD * 2 + cols.length * CARD_W + Math.max(0, cols.length - 1) * COL_GAP;

  const incoming = new Map<string, string[]>();
  for (const [from, to] of linksOf(plan)) incoming.set(to, [...(incoming.get(to) ?? []), from]);

  const curve = (x1: number, y1: number, x2: number, y2: number) => {
    const dx = Math.max(18, (x2 - x1) / 2);
    return `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`;
  };
  const junctions: Junction[] = [];
  const cables: Cable[] = [];
  for (const [to, froms] of incoming) {
    const t = nodes.get(to);
    if (!t) continue;
    const ty = t.y + t.h / 2;
    if (froms.length >= 2) {
      const j: Junction = { id: `j-${to}`, to, from: froms, x: t.x - COL_GAP * 0.42, y: ty };
      junctions.push(j);
      for (const f of froms) {
        const s = nodes.get(f);
        if (s) cables.push({ key: `${f}>${j.id}`, from: f, to, d: curve(s.x + s.w, s.y + s.h / 2, j.x, j.y), into: "junction" });
      }
      cables.push({ key: `${j.id}>${to}`, from: j.id, to, d: `M${j.x},${j.y} L${t.x},${ty}`, into: "out" });
    } else {
      const s = nodes.get(froms[0]);
      if (s) cables.push({ key: `${froms[0]}>${to}`, from: froms[0], to, d: curve(s.x + s.w, s.y + s.h / 2, t.x, ty), into: "card" });
    }
  }
  return { nodes, junctions, cables, columns, width, height };
}

// cableState is how a cable looks: work has gone through it (the part it
// leaves is done), is on its way (that part is running), or not yet.
export function cableState(plan: WorkPlan, c: Cable, j?: Junction): "done" | "running" | "idle" | "failed" {
  const st = (id: string) => plan.phases.find((p) => p.id === id)?.status;
  const ids = c.into === "out" && j ? j.from : [c.from];
  const ss = ids.map(st);
  if (ss.some((s) => s === "failed")) return "failed";
  if (ss.every((s) => s === "done")) return "done";
  if (ss.some((s) => s === "running" || s === "done")) return "running";
  return "idle";
}
