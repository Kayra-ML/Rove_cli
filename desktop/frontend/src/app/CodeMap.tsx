import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { pickFolder, rpc, subscribeEvents } from "~/lib/rpc";
import type { MapGraph, MapHit, MapNeighbors, MapStatus, Workspace } from "~/lib/types";
import { NeuralCanvas } from "~/lib/neuralCanvas";
import { useSessions, useWorkspaces } from "~/hooks/useApi";
import { usePersonaBadges } from "~/hooks/usePersona";
import { spaceOf, type Space } from "~/lib/spaces";
import { usePrefs } from "~/hooks/usePrefs";
import { toast } from "~/lib/toast";
import { t } from "~/lib/i18n";

const LIMITS = [150, 300, 600, 1000];
const LIMIT_KEY = "aether.map.limit";
const EDIT_TOOLS = new Set(["write_file", "patch_file", "create_file", "edit_file", "replace_file", "append_file"]);

interface Props {
  workspace: Workspace | null;
  sessionId?: string;
  // whose conversations the sidebar lists (Agents or Sessions); files picked
  // on the map are shared with the one chosen there
  space?: Space;
  // shown at the top of the sidebar (the Automation page's pickers)
  sideTop?: ReactNode;
}

// relTo turns an agent-supplied path (relative or absolute) into a
// workspace-relative one, matching map node ids.
export function relTo(root: string, p: string): string {
  let s = p.replace(/\\/g, "/").replace(/^\.\//, "");
  const r = root.replace(/\\/g, "/").replace(/\/$/, "");
  if (r && s.startsWith(r + "/")) s = s.slice(r.length + 1);
  return s;
}

export function CodeMap({ workspace, sessionId, space, sideTop }: Props) {
  const { lang } = usePrefs();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const viewRef = useRef<NeuralCanvas | null>(null);
  const [graph, setGraph] = useState<MapGraph | null>(null);
  const [status, setStatus] = useState<MapStatus | null>(null);
  const [loading, setLoading] = useState(false);
  // the daemon will not map a home folder or the disk root: say so on the
  // canvas instead of a raw error in a toast
  const [refused, setRefused] = useState(false);
  const [limit, setLimit] = useState<number>(() => Number(localStorage.getItem(LIMIT_KEY)) || 300);
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<MapHit[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<MapNeighbors | null>(null);
  const [impact, setImpact] = useState<string[] | null>(null);
  const [basket, setBasket] = useState<string[]>([]);
  const [target, setTarget] = useState<string>(sessionId ?? "");
  const [note, setNote] = useState("");
  const [legend, setLegend] = useState<{ group: string; color: string; count: number }[]>([]);
  const [lastFired, setLastFired] = useState<string | null>(null);
  // every conversation of the space, whatever its project: picking one is
  // also how this page moves to that project's map
  const { sessions: wsSessions } = useSessions();
  const { workspaces, reload: reloadWs } = useWorkspaces();
  // a project picked on this page outranks the conversation's own
  const [wsPick, setWsPick] = useState("");
  const { badgeFor } = usePersonaBadges();
  // this project's conversations in the chosen space
  const sessions = useMemo(
    () => (space ? wsSessions.filter((s) => spaceOf(s, badgeFor(s.id)) === space) : wsSessions),
    [wsSessions, badgeFor, space],
  );
  const picked = wsSessions.find((s) => s.id === target);
  const mapWs = (wsPick && workspaces.find((w) => w.id === wsPick))
    || (picked && workspaces.find((w) => w.id === picked.workspaceId))
    || workspace;
  // the disk root is never a project; it is not offered
  const projects = workspaces.filter((w) => w.path !== "/");
  const openFolder = async () => {
    const path = (await pickFolder()).trim();
    if (!path) return;
    try {
      const ws = await rpc<Workspace>("workspace.open", { path, name: path.split(/[/\\]/).filter(Boolean).pop() ?? path });
      await reloadWs();
      setWsPick(ws.id);
    } catch (e) {
      toast(e instanceof Error ? e.message : "open failed", "err");
    }
  };
  const root = mapWs?.path ?? "";
  const wsName = (id: string) => {
    const w = workspaces.find((x) => x.id === id);
    return w && w.path !== "/" ? w.name : "";
  };

  useEffect(() => {
    if (sessionId) setTarget(sessionId);
  }, [sessionId]);

  // mount the renderer once. A canvas that cannot draw (no 2D context) must
  // not take the page down: the sidebar and search keep working without it.
  const [noCanvas, setNoCanvas] = useState(false);
  useEffect(() => {
    const c = canvasRef.current;
    if (!c) return;
    let view: NeuralCanvas;
    try {
      view = new NeuralCanvas(c, { onSelect: (id) => setSelected(id) });
    } catch {
      setNoCanvas(true);
      return;
    }
    view.setForces({ repulsion: 700, linkDistance: 54 });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, []);

  const load = useCallback(async (rebuild = false) => {
    if (!root) return;
    setLoading(true);
    setRefused(false);
    try {
      if (rebuild) await rpc("codemap.build", { workspace: root, full: true });
      const [g, st] = await Promise.all([
        rpc<MapGraph>("codemap.graph", { workspace: root, limit }),
        rpc<MapStatus>("codemap.status", { workspace: root }),
      ]);
      setGraph(g);
      setStatus(st);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "codemap failed";
      if (msg.includes("refusing to index")) setRefused(true);
      else toast(msg, "err");
    } finally {
      setLoading(false);
    }
  }, [root, limit]);

  useEffect(() => {
    setGraph(null);
    setSelected(null);
    setDetail(null);
    setBasket([]);
    setHits([]);
    setImpact(null);
    void load();
  }, [load]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || !graph) return;
    view.readTheme();
    view.setData(graph.nodes, graph.edges);
    setLegend(view.groups().slice(0, 10));
  }, [graph]);

  // Agents writing files make the neuron fire; the map refreshes shortly
  // after so new files and edges show up.
  useEffect(() => {
    if (!root) return;
    let timer = 0;
    const unsub = subscribeEvents("tool.start", (ev) => {
      const p = ev.payload as { name?: string; args?: { path?: string } } | undefined;
      if (!p?.name || !EDIT_TOOLS.has(p.name) || !p.args?.path) return;
      const rel = relTo(root, p.args.path);
      if (viewRef.current?.fire(rel)) setLastFired(rel);
      window.clearTimeout(timer);
      timer = window.setTimeout(() => void load(), 2500);
    });
    return () => {
      unsub();
      window.clearTimeout(timer);
    };
  }, [root, load]);

  useEffect(() => {
    viewRef.current?.select(selected);
    setImpact(null);
    if (!selected || !root) {
      setDetail(null);
      return;
    }
    rpc<MapNeighbors>("codemap.neighbors", { workspace: root, target: selected, limit: 60 })
      .then(setDetail)
      .catch(() => setDetail(null));
  }, [selected, root]);

  useEffect(() => {
    viewRef.current?.mark(hits.map((h) => h.file), "#ffb454");
  }, [hits, graph]);

  useEffect(() => {
    viewRef.current?.setHighlight(impact ?? []);
  }, [impact, graph]);

  const runSearch = async () => {
    const q = query.trim();
    if (!q || !root) {
      setHits([]);
      return;
    }
    try {
      const res = await rpc<{ hits: MapHit[] }>("codemap.query", { workspace: root, q, limit: 20, focus: selected ? [selected] : [] });
      setHits(res.hits ?? []);
      const top = res.hits?.find((h) => graph?.nodes.some((n) => n.id === h.file));
      if (top) {
        setSelected(top.file);
        viewRef.current?.select(top.file, true);
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "search failed", "err");
    }
  };

  const showImpact = async () => {
    if (!selected) return;
    try {
      const res = await rpc<{ files: string[] }>("codemap.impact", { workspace: root, files: [selected], depth: 2 });
      setImpact([selected, ...(res.files ?? [])]);
    } catch (e) {
      toast(e instanceof Error ? e.message : "impact failed", "err");
    }
  };

  const addToBasket = (files: string[]) => setBasket((b) => [...new Set([...b, ...files])]);

  const share = async () => {
    if (!target || basket.length === 0) return;
    try {
      await rpc("codemap.share", { workspace: root, sessionId: target, files: basket, note });
      toast(t("mapShared", lang), "ok");
      setBasket([]);
      setNote("");
    } catch (e) {
      toast(e instanceof Error ? e.message : "share failed", "err");
    }
  };

  const select = (file: string) => {
    setSelected(file);
    viewRef.current?.select(file, true);
  };

  const symbols = useMemo(() => (detail?.symbols ?? []).slice(0, 40), [detail]);

  return (
    <div className="map">
      <div className="map-stage">
        <canvas ref={canvasRef} className="map-canvas" />
        <div className="map-toolbar">
          <select
            className="map-project"
            aria-label={t("mapProject", lang)}
            title={mapWs?.path}
            value={mapWs && mapWs.path !== "/" ? mapWs.id : ""}
            onChange={(e) => {
              if (e.target.value === "__open") void openFolder();
              else setWsPick(e.target.value);
            }}
          >
            <option value="" disabled>{t("mapProject", lang)}…</option>
            {projects.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
            <option value="__open">{t("openFolder", lang)}…</option>
          </select>
          <input
            className="map-search"
            value={query}
            placeholder={t("mapSearch", lang)}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void runSearch();
              if (e.key === "Escape") { setQuery(""); setHits([]); }
            }}
            disabled={!root}
          />
          <select value={limit} onChange={(e) => { const v = Number(e.target.value); setLimit(v); localStorage.setItem(LIMIT_KEY, String(v)); }} disabled={!root}>
            {LIMITS.map((n) => <option key={n} value={n}>{n} {t("mapNodes", lang)}</option>)}
          </select>
          <button type="button" onClick={() => viewRef.current?.fit()} disabled={!graph}>{t("mapFit", lang)}</button>
          <button type="button" onClick={() => void load(true)} disabled={!root || loading}>
            {loading ? "…" : t("mapRebuild", lang)}
          </button>
        </div>
        {status && graph && (
          <div className="map-stats">
            <span>{status.files} {t("mapNodes", lang)} · {status.edges} {t("mapEdges", lang)}</span>
            {graph.truncated && <span> · {graph.nodes.length} {t("mapShown", lang)}</span>}
            <span> · {status.scanMs} ms</span>
            {lastFired && <span className="map-live" title={lastFired}> · ● {t("mapLive", lang)}: {lastFired.split("/").pop()}</span>}
          </div>
        )}
        {legend.length > 0 && (
          <div className="map-legend">
            {legend.map((g) => (
              <span key={g.group} className="map-legend-item">
                <i style={{ background: g.color }} />
                {g.group}
              </span>
            ))}
          </div>
        )}
        <div className="map-hint">{t("mapHint", lang)}</div>
        {(!root || refused) && (
          <div className="map-empty">
            <div>
              <p>{t(root ? "mapRefused" : sessions.some((c) => wsName(c.workspaceId)) ? "mapNoWorkspace" : "mapNoProject", lang)}</p>
              <button type="button" className="primary" onClick={() => void openFolder()}>{t("openFolder", lang)}</button>
            </div>
          </div>
        )}
        {root && noCanvas && <div className="map-empty">{t("mapNoCanvas", lang)}</div>}
        {root && graph && graph.nodes.length === 0 && !loading && <div className="map-empty">{t("mapEmpty", lang)}</div>}
      </div>

      <aside className="map-side">
        {sideTop}
        {space && (
          <section className="map-convs">
            <div className="section-label">{t("convs", lang)}</div>
            <div className="map-muted map-convs-hint">{t("mapConvsHint", lang)}</div>
            {sessions.length === 0 && <div className="map-muted">{t("ctxNoSessions", lang)}</div>}
            {sessions.map((s) => (
              <button
                key={s.id}
                type="button"
                className={`map-row map-conv${s.id === target ? " active" : ""}`}
                aria-pressed={s.id === target}
                onClick={() => { setTarget(s.id === target ? "" : s.id); setWsPick(""); }}
                title={s.title}
              >
                <span className="map-file">{s.title || s.id}</span>
                {(s.id === target || wsName(s.workspaceId)) && (
                  <span className="map-sub">
                    {[wsName(s.workspaceId), s.id === target ? t("mapShareTarget", lang) : ""].filter(Boolean).join(" · ")}
                  </span>
                )}
              </button>
            ))}
          </section>
        )}
        {hits.length > 0 && (
          <section>
            <div className="section-label map-query">{query}</div>
            {hits.slice(0, 10).map((h) => (
              <button key={h.file} className={`map-row${h.file === selected ? " active" : ""}`} onClick={() => select(h.file)}>
                <span className="map-file">{h.file}</span>
                <span className="map-sub">{(h.why ?? []).slice(0, 3).join(" · ")}</span>
              </button>
            ))}
          </section>
        )}

        {!detail && hits.length === 0 && <div className="map-muted">{t("mapSelectFile", lang)}</div>}

        {detail && (
          <section>
            <div className="map-title">{detail.file.split("/").pop()}</div>
            <div className="map-sub">{detail.file} · {detail.lang} · {detail.loc} lines</div>
            <div className="map-actions">
              <button type="button" className="primary" onClick={() => addToBasket([detail.file])}>{t("mapAddContext", lang)}</button>
              <button type="button" onClick={() => void showImpact()}>{t("mapImpact", lang)}</button>
            </div>
            {impact && impact.length > 1 && (
              <div className="map-group">
                <div className="section-label section-label-row">
                  <span>{t("mapImpact", lang)} ({impact.length - 1})</span>
                  <button type="button" className="ghost map-mini" onClick={() => addToBasket(impact)}>+ {t("mapAddContext", lang)}</button>
                </div>
                {impact.slice(1, 16).map((f) => <FileRow key={f} file={f} onClick={select} />)}
              </div>
            )}
            {symbols.length > 0 && (
              <div className="map-group">
                <div className="section-label">{t("mapDefines", lang)} ({detail.symbols?.length ?? 0})</div>
                <div className="map-symbols">
                  {symbols.map((s) => (
                    <span key={`${s.name}:${s.line}`} className="map-sym" title={`${s.kind} · L${s.line}${s.refs ? ` · ${s.refs} refs` : ""}`}>
                      {s.name}
                    </span>
                  ))}
                </div>
              </div>
            )}
            <Rows label={t("mapUses", lang)} files={detail.imports} onClick={select} />
            <Rows label={t("mapUsedBy", lang)} files={detail.importedBy} onClick={select} />
            {detail.calledBy && detail.calledBy.length > 0 && (
              <div className="map-group">
                <div className="section-label">{t("mapCalledBy", lang)} ({detail.calledBy.length})</div>
                {detail.calledBy.slice(0, 20).map((c) => (
                  <button key={`${c.file}:${c.caller}:${c.symbol}`} className="map-row" onClick={() => select(c.file)}>
                    <span className="map-file">{c.caller} → {c.symbol}</span>
                    <span className="map-sub">{c.file}:{c.line}</span>
                  </button>
                ))}
              </div>
            )}
          </section>
        )}

        {basket.length > 0 && (
          <section className="map-basket">
            <div className="section-label">{t("mapBasket", lang)} ({basket.length})</div>
            {basket.map((f) => (
              <div key={f} className="map-basket-row">
                <span className="map-file" title={f}>{f}</span>
                <button type="button" className="ghost map-mini" onClick={() => setBasket((b) => b.filter((x) => x !== f))}>×</button>
              </div>
            ))}
            <label className="map-label">{t("mapShareTo", lang)}</label>
            <select value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value="">—</option>
              {sessions.map((s) => <option key={s.id} value={s.id}>{s.title || s.id}</option>)}
            </select>
            <textarea rows={2} placeholder={t("mapNote", lang)} value={note} onChange={(e) => setNote(e.target.value)} />
            <button type="button" className="primary" disabled={!target} onClick={() => void share()}>{t("mapShare", lang)}</button>
          </section>
        )}
      </aside>
    </div>
  );
}

function Rows({ label, files, onClick }: { label: string; files?: string[]; onClick: (f: string) => void }) {
  if (!files || files.length === 0) return null;
  return (
    <div className="map-group">
      <div className="section-label">{label} ({files.length})</div>
      {files.slice(0, 20).map((f) => <FileRow key={f} file={f} onClick={onClick} />)}
    </div>
  );
}

function FileRow({ file, onClick }: { file: string; onClick: (f: string) => void }) {
  const i = file.lastIndexOf("/");
  return (
    <button className="map-row" onClick={() => onClick(file)} title={file}>
      <span className="map-file">{file.slice(i + 1)}</span>
      <span className="map-sub">{i > 0 ? file.slice(0, i) : "."}</span>
    </button>
  );
}
