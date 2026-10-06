// An office agent's logo: a shape, a small face on it and a color. Stored
// on the agent as "shape:face" plus a hex color; an agent without one gets
// a mark picked from its id, so it keeps the same one everywhere.

export const SHAPES = ["squircle", "circle", "hex", "tri", "drop", "diamond", "shield", "octa"] as const;
// faces are the Rove mark's own terminal prompt: ">-" as on the logo, and
// its kin (">_", a cursor block, ">>", "<>")
export const FACES = ["prompt", "under", "cursor", "double", "angles"] as const;
export type Shape = (typeof SHAPES)[number];
export type Face = (typeof FACES)[number];

// muted, told apart at a glance, readable with a dark face on top
export const MARK_COLORS = [
  "#e5735f", "#e3a64a", "#9cc461", "#5cc79a", "#43b3a8",
  "#5aa3e0", "#7f95b5", "#dd7393", "#cfae82", "#9aa1ad",
] as const;

export type Mark = { shape: Shape; face: Face; color: string };

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

// What an expert looks like before anyone picks: the shape says the field
// (the shield is security's alone), the face is the logo's prompt, the
// color comes from the agent's id so two agents of one field differ.
const SHAPE_FOR: Record<string, Shape> = {
  security: "shield",
  frontend: "squircle", mobile: "squircle", uiux: "squircle",
  "go-backend": "hex", python: "hex",
  architect: "circle",
  database: "octa", "data-ml": "octa",
  devops: "diamond", performance: "diamond",
  qa: "tri", debugger: "tri", reviewer: "tri",
  product: "drop", writer: "drop", marketing: "drop",
};
const FREE_SHAPES = SHAPES.filter((s) => s !== "shield");

// defaultMark is the mark an agent gets before anyone picks one.
export function defaultMark(seed: string, character?: string): Mark {
  const h = hash(seed || "agent");
  return {
    shape: (character && SHAPE_FOR[character]) || FREE_SHAPES[h % FREE_SHAPES.length],
    face: "prompt",
    color: MARK_COLORS[(h >>> 16) % MARK_COLORS.length],
  };
}

// markOf reads a stored mark, filling what is missing or unknown from the
// agent's id and starting character.
export function markOf(stored: string | undefined, color: string | undefined, seed: string, character?: string): Mark {
  const d = defaultMark(seed, character);
  const [s, f] = (stored ?? "").split(":");
  return {
    shape: (SHAPES as readonly string[]).includes(s) ? (s as Shape) : d.shape,
    face: (FACES as readonly string[]).includes(f) ? (f as Face) : d.face,
    color: /^#[0-9a-f]{6}$/i.test(color ?? "") ? color! : d.color,
  };
}

export const markString = (m: Mark) => `${m.shape}:${m.face}`;

// Shapes on a 32×32 grid. Corners are rounded by stroking each shape in its
// own color with round joins, so every shape reads as soft without a path
// per radius.
export const SHAPE_PATH: Record<Shape, string> = {
  squircle: "M10 3.5h12a6.5 6.5 0 0 1 6.5 6.5v12a6.5 6.5 0 0 1-6.5 6.5H10A6.5 6.5 0 0 1 3.5 22V10A6.5 6.5 0 0 1 10 3.5z",
  circle: "M16 3.5a12.5 12.5 0 1 1 0 25a12.5 12.5 0 1 1 0-25z",
  hex: "M16 3.2l11 6.4v12.8l-11 6.4l-11-6.4V9.6z",
  tri: "M16 4.5l12.5 21.5h-25z",
  drop: "M16 3.5c5 5.6 10.8 10.6 10.8 15.8a10.8 10.8 0 0 1-21.6 0c0-5.2 5.8-10.2 10.8-15.8z",
  diamond: "M16 3l13 13l-13 13l-13-13z",
  shield: "M16 3.5l11.5 4.2v8.3c0 6.7-4.8 11.4-11.5 13.5c-6.7-2.1-11.5-6.8-11.5-13.5V7.7z",
  octa: "M11 3.5h10l7.5 7.5v10l-7.5 7.5H11l-7.5-7.5V11z",
};

// where the face sits: lower on shapes whose mass is low
const FACE_Y: Record<Shape, number> = { squircle: 16, circle: 16, hex: 16, tri: 19.5, drop: 19, diamond: 16, shield: 15.5, octa: 16 };

export type FacePart = { kind: "circle"; cx: number; cy: number; r: number } | { kind: "path"; d: string; stroke: number } | { kind: "rect"; x: number; y: number; w: number; h: number };

export function faceParts(shape: Shape, face: Face): FacePart[] {
  const y = FACE_Y[shape];
  const k = shape === "tri" ? 0.8 : 1; // the triangle is narrow up top
  const chev = (x: number) => `M${x - 1.6 * k} ${y - 3 * k} l${3 * k} ${3 * k} l${-3 * k} ${3 * k}`;
  const w = 2.1;
  switch (face) {
    case "prompt": // ">-", the logo
      return [{ kind: "path", d: chev(11.5), stroke: w }, { kind: "path", d: `M${15.5} ${y} h${5 * k}`, stroke: w }];
    case "under": // ">_"
      return [{ kind: "path", d: chev(11.5), stroke: w }, { kind: "path", d: `M${15.5} ${y + 3 * k} h${5 * k}`, stroke: w }];
    case "cursor": // ">▌"
      return [{ kind: "path", d: chev(11.5), stroke: w }, { kind: "rect", x: 16, y: y - 3.2 * k, w: 3.4 * k, h: 6.4 * k }];
    case "double": // ">>"
      return [{ kind: "path", d: chev(12.5), stroke: w }, { kind: "path", d: chev(18), stroke: w }];
    case "angles": // "<>"
      return [
        { kind: "path", d: `M${14 - 0.2} ${y - 3 * k} l${-3 * k} ${3 * k} l${3 * k} ${3 * k}`, stroke: w },
        { kind: "path", d: `M${18 + 0.2} ${y - 3 * k} l${3 * k} ${3 * k} l${-3 * k} ${3 * k}`, stroke: w },
      ];
  }
}
