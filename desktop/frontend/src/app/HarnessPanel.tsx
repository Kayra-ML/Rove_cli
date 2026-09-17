import { useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";

interface GoalExecProfile {
  mode: "manual" | "auto";
  context: number;
  execution: number;
  tools: number;
  verify: number;
  recovery: number;
  version: number;
  reason?: string;
  composerReason?: string;
  createdAt?: string;
  updatedAt?: string;
}

interface HarnessProfile {
  goalId?: string;
  cardId?: string;
  current: GoalExecProfile;
  mutations?: HarnessMutation[];
  createdAt: string;
  updatedAt: string;
}

interface HarnessMutation {
  goalId: string;
  cardId: string;
  iteration: number;
  reason: string;
  oldProfile: GoalExecProfile;
  newProfile: GoalExecProfile;
  timestamp: string;
  result: string;
}

const CTX = {
  SelectiveContext: 1 << 0,
  LargeContext:     1 << 1,
  FreshContext:     1 << 2,
  RepositoryMap:    1 << 3,
  MemoryHeavy:      1 << 4,
};

const EXEC = {
  Direct:      1 << 0,
  PlanExecute: 1 << 1,
  GoalLoop:    1 << 2,
  Parallel:    1 << 3,
  Delegated:   1 << 4,
  Sandboxed:   1 << 5,
};

const TOOLS = {
  SequentialTools: 1 << 0,
  ParallelTools:   1 << 1,
  RestrictedTools: 1 << 2,
  CodingTools:     1 << 3,
  ResearchTools:   1 << 4,
};

const VERIFY = {
  FastVerify:        1 << 0,
  TestVerify:        1 << 1,
  IndependentReview: 1 << 2,
  DoubleReview:      1 << 3,
  ArtifactVerify:    1 << 4,
};

const RECOVERY = {
  Retry:            1 << 0,
  Replan:           1 << 1,
  ModelFallback:    1 << 2,
  ContextReset:     1 << 3,
  CheckpointResume: 1 << 4,
  Escalation:       1 << 5,
};

function activeFlags(val: number, flags: Record<string, number>): string[] {
  return Object.entries(flags)
    .filter(([, bit]) => (val & bit) !== 0)
    .map(([name]) => name);
}

function pretty(name: string) {
  return name.replace(/([A-Z])/g, " $1").trim();
}

function Badge({ name }: { name: string }) {
  return <span className="hp-badge">{pretty(name)}</span>;
}

function TacticRow({ label, names }: { label: string; names: string[] }) {
  if (names.length === 0) return null;
  return (
    <div className="hp-row">
      <span className="hp-row-label">{label}</span>
      <div className="hp-flags">
        {names.map((n) => <Badge key={n} name={n} />)}
      </div>
    </div>
  );
}

function MutationTimeline({ mutations }: { mutations: HarnessMutation[] }) {
  if (mutations.length === 0) return null;
  return (
    <div className="hp-muts">
      <div className="section-label" style={{ padding: "10px 0 6px" }}>
        Mutations · {mutations.length}
      </div>
      {mutations.map((m, i) => (
        <div key={i} className="hp-mut">
          <div className="hp-mut-head">
            <span>iter {m.iteration}</span>
            <span>{m.timestamp ? new Date(m.timestamp).toLocaleTimeString() : ""}</span>
          </div>
          <p>{m.reason}</p>
        </div>
      ))}
    </div>
  );
}

function ProfileView({
  profile,
  onEdit,
}: {
  profile: GoalExecProfile;
  onEdit?: () => void;
}) {
  return (
    <div>
      <div className="hp-meta">
        <span className={`hp-mode ${profile.mode === "auto" ? "auto" : "manual"}`}>
          {profile.mode === "auto" ? "Auto" : "Manual"}
        </span>
        {profile.version > 0 && <span className="hp-ver">v{profile.version}</span>}
        {onEdit && (
          <button className="ghost" style={{ marginLeft: "auto", fontSize: 11 }} onClick={onEdit}>
            Edit
          </button>
        )}
      </div>
      {profile.reason && <p className="hp-reason">{profile.reason}</p>}
      {!profile.reason && profile.composerReason && <p className="hp-reason">{profile.composerReason}</p>}
      <TacticRow label="Context"   names={activeFlags(profile.context, CTX)} />
      <TacticRow label="Execution" names={activeFlags(profile.execution, EXEC)} />
      <TacticRow label="Tools"     names={activeFlags(profile.tools, TOOLS)} />
      <TacticRow label="Verify"    names={activeFlags(profile.verify, VERIFY)} />
      <TacticRow label="Recovery"  names={activeFlags(profile.recovery, RECOVERY)} />
    </div>
  );
}

function ProfileEditor({
  profile,
  onSave,
  onCancel,
}: {
  profile: GoalExecProfile;
  onSave: (p: GoalExecProfile) => void;
  onCancel: () => void;
}) {
  const [p, setP] = useState<GoalExecProfile>({ ...profile, mode: "manual" });

  const toggle = (
    field: "context" | "execution" | "tools" | "verify" | "recovery",
    bit: number,
  ) => {
    setP((prev) => ({ ...prev, [field]: prev[field] ^ bit }));
  };

  const FlagGroup = ({
    label,
    field,
    flags,
  }: {
    label: string;
    field: "context" | "execution" | "tools" | "verify" | "recovery";
    flags: Record<string, number>;
  }) => (
    <div className="hp-edit-group">
      <div className="hp-row-label">{label}</div>
      <div className="hp-flags">
        {Object.entries(flags).map(([name, bit]) => {
          const active = (p[field] & bit) !== 0;
          return (
            <button
              key={name}
              type="button"
              className={`hp-flag-btn${active ? " on" : ""}`}
              onClick={() => toggle(field, bit)}
            >
              {pretty(name)}
            </button>
          );
        })}
      </div>
    </div>
  );

  return (
    <div>
      <FlagGroup label="Context"   field="context"   flags={CTX} />
      <FlagGroup label="Execution" field="execution" flags={EXEC} />
      <FlagGroup label="Tools"     field="tools"     flags={TOOLS} />
      <FlagGroup label="Verify"    field="verify"    flags={VERIFY} />
      <FlagGroup label="Recovery"  field="recovery"  flags={RECOVERY} />
      <div style={{ display: "flex", gap: 8, marginTop: 12 }}>
        <button className="primary" style={{ fontSize: 12 }} onClick={() => onSave(p)}>Save</button>
        <button className="ghost" style={{ fontSize: 12 }} onClick={onCancel}>Cancel</button>
      </div>
    </div>
  );
}

interface HarnessPanelProps {
  goalId?: string;
  cardId?: string;
  collapsible?: boolean;
}

export default function HarnessPanel({ goalId, cardId, collapsible = true }: HarnessPanelProps) {
  const [hp, setHp] = useState<HarnessProfile | null>(null);
  const [mutations, setMutations] = useState<HarnessMutation[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(!collapsible);
  const [editing, setEditing] = useState(false);
  const [presets, setPresets] = useState<Record<string, GoalExecProfile>>({});
  const [applying, setApplying] = useState("");

  useEffect(() => {
    void rpc<Record<string, GoalExecProfile>>("harness.presets")
      .then((p) => { if (p && typeof p === "object") setPresets(p); })
      .catch(() => {});
  }, []);

  const load = async () => {
    if (!goalId && !cardId) return;
    setLoading(true);
    setError("");
    try {
      const raw = await rpc("harness.get", goalId ? { goalId } : { cardId });
      if (raw && typeof raw === "object") {
        setHp(raw as HarnessProfile);
      } else if (typeof raw === "string" && raw !== "") {
        setHp(JSON.parse(raw));
      }
      if (goalId) {
        const muts = await rpc("harness.mutations", { goalId });
        if (Array.isArray(muts)) setMutations(muts);
      }
    } catch {
      setHp(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) void load();
    const interval = setInterval(() => { if (open) void load(); }, 5000);
    return () => clearInterval(interval);
  }, [open, goalId, cardId]); // eslint-disable-line react-hooks/exhaustive-deps

  const applyPreset = async (key: string, profile: GoalExecProfile) => {
    if (!goalId && !cardId) return;
    setApplying(key);
    setError("");
    try {
      const wrapped = {
        ...(hp ?? {}),
        current: {
          ...profile,
          mode: "auto" as const,
          reason: profile.reason || profile.composerReason,
        },
      };
      await rpc("harness.set", {
        ...(goalId ? { goalId } : {}),
        ...(cardId ? { cardId } : {}),
        profile: wrapped,
      });
      setEditing(false);
      await load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setApplying("");
    }
  };

  const handleSave = async (updated: GoalExecProfile) => {
    try {
      const profileStr = JSON.stringify({ ...(hp ?? {}), current: updated });
      await rpc("harness.set", {
        ...(goalId ? { goalId } : {}),
        ...(cardId ? { cardId } : {}),
        profile: JSON.parse(profileStr),
      });
      setEditing(false);
      await load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="hp">
      <button
        type="button"
        className="hp-head"
        onClick={() => collapsible && setOpen((v) => !v)}
      >
        <span>Harness</span>
        {hp && <span className={`hp-mode ${hp.current.mode === "auto" ? "auto" : "manual"}`}>{hp.current.mode}</span>}
        {mutations.length > 0 && <span className="hp-ver">{mutations.length} mut</span>}
        {collapsible && <span className="hp-caret">{open ? "▴" : "▾"}</span>}
      </button>
      {open && (
        <div className="hp-body">
          {loading && <p className="hp-reason">Loading…</p>}
          {error && !hp && <p className="hp-reason">{error}</p>}
          {Object.keys(presets).length > 0 && (
            <div className="hp-presets">
              {([
                ["small", "Small bug"],
                ["large", "Large refactor"],
                ["hard", "Hard / long"],
              ] as const).map(([key, label]) => (
                presets[key] ? (
                  <button
                    key={key}
                    type="button"
                    className="hp-preset"
                    disabled={!!applying || (!goalId && !cardId)}
                    onClick={() => void applyPreset(key, presets[key])}
                    title={presets[key].reason || presets[key].composerReason}
                  >
                    {applying === key ? "…" : label}
                  </button>
                ) : null
              ))}
            </div>
          )}
          {hp && !editing && (
            <>
              <ProfileView profile={hp.current} onEdit={() => setEditing(true)} />
              <MutationTimeline mutations={mutations} />
            </>
          )}
          {!hp && !loading && !error && (goalId || cardId) && (
            <p className="hp-reason">Pick a preset or edit a profile to start.</p>
          )}
          {hp && editing && (
            <ProfileEditor
              profile={hp.current}
              onSave={(p) => void handleSave(p)}
              onCancel={() => setEditing(false)}
            />
          )}
        </div>
      )}
    </div>
  );
}
