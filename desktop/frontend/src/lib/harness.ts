import type { Goal, HarnessCategory, HarnessCategoryKey, HarnessProfile } from "./types";

// A goal's harness profile is five bitmasks, one per category; the daemon's
// catalog (harness.catalog) says which bit each tactic is, so nothing here
// knows the numbers.

export function has(p: HarnessProfile, cat: HarnessCategoryKey, bit: number): boolean {
  return ((p[cat] ?? 0) & bit) !== 0;
}

// toggle is p with one tactic switched, as a profile picked by hand.
export function toggle(p: HarnessProfile, cat: HarnessCategoryKey, bit: number): HarnessProfile {
  return { ...p, mode: "manual", [cat]: (p[cat] ?? 0) ^ bit };
}

export type TacticChange = { cat: HarnessCategoryKey; key: string; on: boolean };

// changes lists the tactics turned on and off from one profile to the
// next, in catalog order: what a mutation did, in words a person reads.
export function changes(catalog: HarnessCategory[], from: HarnessProfile, to: HarnessProfile): TacticChange[] {
  const out: TacticChange[] = [];
  for (const c of catalog) {
    for (const t of c.tactics) {
      const was = has(from, c.key, t.bit);
      const now = has(to, c.key, t.bit);
      if (was !== now) out.push({ cat: c.key, key: t.key, on: now });
    }
  }
  return out;
}

// tokens shortens a token count the way the usage panel does: 64k, 1.2M.
export function tokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 ? 1 : 0)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

// A goal runs again from where it stopped; these are the states it can be
// started from (pending: never ran or waits; the rest: stopped short) —
// except a goal that used all its iterations, which would only stop again.
export function canDrive(g: Pick<Goal, "status" | "iteration" | "completionContract">): boolean {
  const max = g.completionContract?.maxIterations ?? 0;
  if (max > 0 && g.iteration >= max) return false;
  return g.status === "pending" || g.status === "canceled" || g.status === "blocked" || g.status === "failed";
}
