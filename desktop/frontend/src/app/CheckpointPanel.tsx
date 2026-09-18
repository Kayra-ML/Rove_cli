import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import { toast } from "~/lib/toast";

interface Snapshot {
  id: string;
  label: string;
  ref: string;
  createdAt: string;
}

interface Props {
  workspacePath?: string;
}

export function CheckpointPanel({ workspacePath }: Props) {
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [loading, setLoading] = useState(false);
  const [taking, setTaking] = useState(false);

  const reload = useCallback(async () => {
    if (!workspacePath) { setSnapshots([]); return; }
    try {
      const snaps = await rpc<Snapshot[]>("checkpoint.list", { path: workspacePath });
      setSnapshots(Array.isArray(snaps) ? snaps : []);
    } catch {
      setSnapshots([]);
    }
  }, [workspacePath]);

  useEffect(() => { void reload(); }, [reload]);

  async function takeSnapshot() {
    if (!workspacePath) return;
    setTaking(true);
    try {
      await rpc("checkpoint.take", { path: workspacePath, label: "" });
      toast("Snapshot taken", "ok");
      await reload();
    } catch (err) {
      const msg = err instanceof Error ? err.message : "snapshot failed";
      toast(msg, "err");
    } finally {
      setTaking(false);
    }
  }

  async function restoreSnapshot(ref: string) {
    if (!workspacePath) return;
    setLoading(true);
    try {
      await rpc("checkpoint.restore", { path: workspacePath, ref });
      toast(`Restored ${ref}`, "ok");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "restore failed";
      toast(msg, "err");
    } finally {
      setLoading(false);
    }
  }

  if (!workspacePath) return null;

  return (
    <div style={{ padding: "4px 0" }}>
      <div className="section-label section-label-row">
        <span>Snapshots</span>
        <button
          type="button"
          className="icon-btn"
          title="Take snapshot"
          disabled={taking || loading}
          onClick={() => void takeSnapshot()}
          style={{ fontSize: 13 }}
        >
          {taking ? "…" : "+"}
        </button>
      </div>
      {snapshots.length === 0 ? (
        <div style={{ padding: "4px 14px", color: "var(--faint)", fontSize: 12 }}>
          No snapshots yet
        </div>
      ) : (
        snapshots.map((s) => (
          <div
            key={s.ref}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 6,
              padding: "3px 12px",
            }}
          >
            <span
              style={{
                flex: 1,
                fontSize: 11,
                color: "var(--muted)",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
              title={s.label || s.ref}
            >
              <span style={{ color: "var(--text)", fontWeight: 500 }}>{s.ref}</span>
              {s.label ? (
                <span style={{ marginLeft: 4 }}>{s.label}</span>
              ) : null}
            </span>
            <button
              type="button"
              className="ghost"
              style={{ fontSize: 10, padding: "2px 6px", flexShrink: 0 }}
              disabled={loading || taking}
              onClick={() => void restoreSnapshot(s.ref)}
              title={`Restore ${s.ref}`}
            >
              restore
            </button>
          </div>
        ))
      )}
    </div>
  );
}