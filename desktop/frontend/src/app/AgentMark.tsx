import { SHAPE_PATH, faceParts, markOf, type Mark } from "~/lib/agentmark";

interface Props {
  // the agent's stored mark and color; seed picks one when they are empty
  mark?: string;
  color?: string;
  seed: string;
  // the starting character: an unpicked shape follows its field
  character?: string;
  size?: number;
  // a small dot on the corner: the agent is working, or finished unseen
  status?: "running" | "done" | null;
  title?: string;
}

// AgentMark draws an office agent's logo: its shape in its color with a
// small face. It stands for the agent in the side list, the agent card,
// the settings and the chat header.
export function AgentMark({ mark, color, seed, character, size = 22, status, title }: Props) {
  const m = markOf(mark, color, seed, character);
  return <MarkSvg m={m} size={size} status={status} title={title} />;
}

export function MarkSvg({ m, size = 22, status, title }: { m: Mark; size?: number; status?: "running" | "done" | null; title?: string }) {
  const ink = "rgba(12, 12, 14, 0.78)";
  return (
    <span className={`agent-mark${status ? ` ${status}` : ""}`} style={{ width: size, height: size }} title={title} aria-hidden={title ? undefined : true}>
      <svg viewBox="0 0 32 32" width={size} height={size}>
        <path d={SHAPE_PATH[m.shape]} fill={m.color} stroke={m.color} strokeWidth={2.4} strokeLinejoin="round" />
        {faceParts(m.shape, m.face).map((p, i) =>
          p.kind === "circle" ? <circle key={i} cx={p.cx} cy={p.cy} r={p.r} fill={ink} />
            : p.kind === "rect" ? <rect key={i} x={p.x} y={p.y} width={p.w} height={p.h} rx={1} fill={ink} />
            : <path key={i} d={p.d} fill="none" stroke={ink} strokeWidth={p.stroke} strokeLinecap="round" strokeLinejoin="round" />,
        )}
      </svg>
      {status && <span className="agent-mark-dot" />}
    </span>
  );
}
