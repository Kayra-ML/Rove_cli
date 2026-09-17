import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc } from "~/lib/rpc";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

interface Props {
  root: string;
}

type Node = { name: string; path: string; dir: boolean; kids?: Node[] };

function nest(paths: string[]): Node[] {
  const root: Node[] = [];
  const idx = new Map<string, Node>();
  const sorted = [...paths].sort();
  for (const raw of sorted) {
    const dir = raw.endsWith("/");
    const clean = dir ? raw.slice(0, -1) : raw;
    const parts = clean.split("/").filter(Boolean);
    let acc = "";
    let bucket = root;
    parts.forEach((part, i) => {
      acc = acc ? `${acc}/${part}` : part;
      const isLast = i === parts.length - 1;
      const isDir = !isLast || dir;
      let n = idx.get(acc);
      if (!n) {
        n = { name: part, path: acc, dir: isDir, kids: isDir ? [] : undefined };
        idx.set(acc, n);
        bucket.push(n);
      }
      bucket = n.kids ?? [];
    });
  }
  return root;
}

function Row({
  n,
  depth,
  open,
  toggle,
  onFile,
  active,
}: {
  n: Node;
  depth: number;
  open: Set<string>;
  toggle: (p: string) => void;
  onFile: (p: string) => void;
  active: string | null;
}) {
  const expanded = open.has(n.path);
  return (
    <>
      <button
        className={`nav-item file-row${active === n.path ? " active" : ""}`}
        style={{ paddingLeft: 10 + depth * 12 }}
        onClick={() => (n.dir ? toggle(n.path) : onFile(n.path))}
        title={n.path}
      >
        <span className="file-ico">{n.dir ? (expanded ? "▾" : "▸") : "·"}</span>
        {n.name}
      </button>
      {n.dir && expanded && n.kids?.map((c) => (
        <Row key={c.path} n={c} depth={depth + 1} open={open} toggle={toggle} onFile={onFile} active={active} />
      ))}
    </>
  );
}

export function FileTree({ root }: Props) {
  const { lang } = usePrefs();
  const [paths, setPaths] = useState<string[]>([]);
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const [preview, setPreview] = useState<{ path: string; content: string } | null>(null);
  const [err, setErr] = useState("");

  const load = useCallback(async () => {
    if (!root) return;
    try {
      const list = await rpc<string[]>("fs.tree", { path: root });
      setPaths(list ?? []);
      setErr("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "tree failed");
      setPaths([]);
    }
  }, [root]);

  useEffect(() => { void load(); }, [load]);

  const tree = useMemo(() => nest(paths), [paths]);

  const toggle = (p: string) => {
    setOpen((s) => {
      const n = new Set(s);
      if (n.has(p)) n.delete(p); else n.add(p);
      return n;
    });
  };

  const onFile = async (rel: string) => {
    try {
      const full = `${root.replace(/\/$/, "")}/${rel}`;
      const r = await rpc<{ content: string }>("fs.read", { path: full });
      setPreview({ path: rel, content: r.content ?? "" });
    } catch (e) {
      setErr(e instanceof Error ? e.message : "read failed");
    }
  };

  return (
    <div className="file-tree">
      <div className="section-label section-label-row">
        <span>Files</span>
        <button className="icon-btn" title="reload" onClick={() => void load()}>↻</button>
      </div>
      {err && <div style={{ padding: "4px 12px", color: "var(--bad)", fontSize: 11 }}>{err}</div>}
      <div className="file-list">
        {tree.length === 0 ? (
          <div style={{ padding: "4px 14px", color: "var(--faint)", fontSize: 12 }}>{t("noneYet", lang)}</div>
        ) : (
          tree.map((n) => (
            <Row key={n.path} n={n} depth={0} open={open} toggle={toggle} onFile={(p) => void onFile(p)} active={preview?.path ?? null} />
          ))
        )}
      </div>
      {preview && (
        <div className="file-preview">
          <div className="section-label section-label-row">
            <span>{preview.path}</span>
            <button className="ghost" style={{ fontSize: 11 }} onClick={() => setPreview(null)}>×</button>
          </div>
          <pre className="file-preview-body">{preview.content}</pre>
        </div>
      )}
    </div>
  );
}
