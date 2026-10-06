import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { rpc, subscribeEvents } from "~/lib/rpc";
import { t, type Lang } from "~/lib/i18n";
import { toast } from "~/lib/toast";
import { useDialog } from "~/hooks/useDialog";

// A turn's changes: every file the agent wrote in its last run that changed
// files, diffed against what it held before, each one kept or taken back.

type EditFile = {
  path: string;
  status: "pending" | "accepted" | "reverted";
  change: "added" | "modified" | "deleted" | "unchanged";
  added: number;
  removed: number;
  diff: string;
};
type EditsView = { runId: string; files: EditFile[] };

// useEdits follows a chat's latest changed turn: loaded on open and again
// whenever a run that changed files ends.
export function useEdits(sessionId: string | null | undefined) {
  const [view, setView] = useState<EditsView | null>(null);
  const reload = useCallback(async () => {
    if (!sessionId) { setView(null); return; }
    try {
      setView(await rpc<EditsView>("edits.list", { sessionId }));
    } catch {
      setView(null);
    }
  }, [sessionId]);
  useEffect(() => {
    void reload();
    if (!sessionId) return;
    return subscribeEvents(`session.${sessionId}`, (ev) => {
      const edits = Number((ev.payload as { edits?: number } | undefined)?.edits ?? 0);
      if (ev.type === "run.done" && edits > 0) void reload();
    });
  }, [sessionId, reload]);
  return { view, setView, reload };
}

// act accepts or takes back one file (or every pending one) of a turn.
async function act(kind: "accept" | "revert", sessionId: string, runId: string, path?: string): Promise<EditsView> {
  return rpc<EditsView>(`edits.${kind}`, { sessionId, runId, path: path ?? "" });
}

const pending = (v: EditsView | null) => (v?.files ?? []).filter((f) => f.status === "pending" && f.change !== "unchanged");

// ChangesBar sits above the composer while a turn's changes wait for review.
export function ChangesBar({ sessionId, view, setView, onOpen, lang, compact }: {
  sessionId: string;
  view: EditsView | null;
  setView: (v: EditsView) => void;
  onOpen: () => void;
  lang: Lang;
  compact?: boolean;
}) {
  const open = pending(view);
  if (!view || open.length === 0) return null;
  const added = open.reduce((n, f) => n + f.added, 0);
  const removed = open.reduce((n, f) => n + f.removed, 0);
  const acceptAll = async () => {
    try { setView(await act("accept", sessionId, view.runId)); } catch (e) { toast(e instanceof Error ? e.message : "accept", "err"); }
  };
  return (
    <div className={`status-stack changes-bar${compact ? " compact" : ""}`}>
      <span className="changes-count">{open.length} {t("reviewFiles", lang)}</span>
      <span className="diff-add">+{added}</span>
      <span className="diff-del">−{removed}</span>
      <span className="tw-spacer" />
      <button type="button" className="ghost" onClick={onOpen}>{t("reviewOpen", lang)}</button>
      <button type="button" className="ghost" onClick={() => void acceptAll()}>{t("reviewAcceptAll", lang)}</button>
    </div>
  );
}

// ReviewSheet shows a turn's files and their diffs over the page.
export function ReviewSheet({ sessionId, view, setView, onClose, lang }: {
  sessionId: string;
  view: EditsView | null;
  setView: (v: EditsView) => void;
  onClose: () => void;
  lang: Lang;
}) {
  const sheet = useDialog<HTMLDivElement>();
  const files = (view?.files ?? []).filter((f) => f.change !== "unchanged");
  const [sel, setSel] = useState(0);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const cur = files[Math.min(sel, files.length - 1)];
  const run = async (kind: "accept" | "revert", path?: string) => {
    if (!view || busy) return;
    setBusy(true);
    try {
      setView(await act(kind, sessionId, view.runId, path));
      window.dispatchEvent(new Event("rove:files"));
    } catch (e) {
      toast(e instanceof Error ? e.message : kind, "err");
    } finally {
      setBusy(false);
    }
  };
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="review-sheet" role="dialog" aria-modal="true" aria-label={t("reviewTitle", lang)} ref={sheet}>
        <header className="review-head">
          <h2>{t("reviewTitle", lang)}</h2>
          <span className="map-muted">{files.length} {t("reviewFiles", lang)}</span>
          <span className="tw-spacer" />
          <button type="button" className="ghost" disabled={busy || pending(view).length === 0} onClick={() => void run("revert")}>{t("reviewRevertAll", lang)}</button>
          <button type="button" className="primary" disabled={busy || pending(view).length === 0} onClick={() => void run("accept")}>{t("reviewAcceptAll", lang)}</button>
          <button type="button" className="tree-icon" title={t("close", lang)} onClick={onClose}>×</button>
        </header>
        {files.length === 0 ? (
          <p className="map-muted review-empty">{t("reviewNone", lang)}</p>
        ) : (
          <div className="review-body">
            <nav className="review-files">
              {files.map((f, i) => (
                <button type="button" key={f.path} className={`review-file${i === sel ? " on" : ""} ${f.status}`} onClick={() => setSel(i)}>
                  <span className={`review-kind ${f.change}`}>{f.change === "added" ? "A" : f.change === "deleted" ? "D" : "M"}</span>
                  <span className="review-path" title={f.path}>{f.path}</span>
                  <span className="diff-add">+{f.added}</span>
                  <span className="diff-del">−{f.removed}</span>
                </button>
              ))}
            </nav>
            {cur && (
              <section className="review-diff">
                <div className="review-diff-head">
                  <code>{cur.path}</code>
                  <span className={`review-status ${cur.status}`}>{t(`reviewStatus_${cur.status}`, lang)}</span>
                  <span className="tw-spacer" />
                  {cur.status !== "reverted" && (
                    <button type="button" className="ghost" disabled={busy} onClick={() => void run("revert", cur.path)}>{t("reviewRevert", lang)}</button>
                  )}
                  {cur.status === "pending" && (
                    <button type="button" className="ghost" disabled={busy} onClick={() => void run("accept", cur.path)}>{t("reviewAccept", lang)}</button>
                  )}
                </div>
                <pre className="diff-view">
                  {cur.diff.split("\n").map((line, i) => (
                    <div key={i} className={line.startsWith("@@") ? "hunk" : line.startsWith("+++") || line.startsWith("---") ? "meta" : line.startsWith("+") ? "add" : line.startsWith("-") ? "del" : ""}>{line || " "}</div>
                  ))}
                </pre>
              </section>
            )}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
