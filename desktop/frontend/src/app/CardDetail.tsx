import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { Card, LogEntry, Artifact } from "~/lib/types";
import HarnessPanel from "./HarnessPanel";
import { toast } from "~/lib/toast";

interface Props {
  card: Card | null;
  onClose: () => void;
}

interface GitLogEntry {
  hash: string;
  author: string;
  date: string;
  subject: string;
}

interface Hunk {
  header: string;
  lines: string[];
  raw: string;
}

/** Split a unified diff into per-hunk objects */
function parseHunks(diff: string): Hunk[] {
  const hunks: Hunk[] = [];
  let cur: Hunk | null = null;
  let fileHeader = "";
  for (const line of diff.split("\n")) {
    if (line.startsWith("diff ") || line.startsWith("index ") ||
        line.startsWith("--- ") || line.startsWith("+++ ")) {
      fileHeader += line + "\n";
      cur = null;
    } else if (line.startsWith("@@")) {
      if (cur) hunks.push(cur);
      cur = { header: line, lines: [], raw: fileHeader + line + "\n" };
    } else if (cur) {
      cur.lines.push(line);
      cur.raw += line + "\n";
    }
  }
  if (cur) hunks.push(cur);
  return hunks;
}

export function CardDetail({ card, onClose }: Props) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [gitLog, setGitLog] = useState<GitLogEntry[]>([]);
  const [gitStatus, setGitStatus] = useState<{ branch?: string; dirty?: boolean; changed?: string[]; untracked?: string[] } | null>(null);
  const [gitDiff, setGitDiff] = useState("");
  const [commitMsg, setCommitMsg] = useState("");
  const [prTitle, setPrTitle] = useState("");
  const [prBody, setPrBody] = useState("");
  const [prBase, setPrBase] = useState("main");
  const [prLoading, setPrLoading] = useState(false);
  const [artKind, setArtKind] = useState("file");
  const [artLabel, setArtLabel] = useState("");
  const [artPath, setArtPath] = useState("");

  const loadLogs = useCallback(async () => {
    if (!card) return;
    try {
      setLogs(await rpc<LogEntry[]>("card.logs", { id: card.id }));
    } catch { setLogs([]); }
  }, [card]);

  const loadGitLog = useCallback(async () => {
    if (!card?.worktreePath) return;
    try {
      setGitLog(await rpc<GitLogEntry[]>("git.log", { path: card.worktreePath, limit: 10 }));
    } catch { setGitLog([]); }
    try {
      setGitStatus(await rpc("git.status", { path: card.worktreePath }));
    } catch { setGitStatus(null); }
    try {
      const d = await rpc<{ diff?: string }>("git.diff", { path: card.worktreePath });
      setGitDiff(d?.diff ?? "");
    } catch { setGitDiff(""); }
  }, [card]);

  useEffect(() => {
    void loadLogs();
    void loadGitLog();
  }, [loadLogs, loadGitLog]);

  if (!card) return null;

  const commit = async () => {
    if (!card.worktreePath || !commitMsg) return;
    await rpc("git.commit", { path: card.worktreePath, message: commitMsg });
    setCommitMsg("");
    await loadGitLog();
  };

  const hunks = gitDiff ? parseHunks(gitDiff) : [];

  const applyHunk = async (hunk: Hunk) => {
    if (!card.worktreePath) return;
    try {
      await rpc("git.applyHunk", { path: card.worktreePath, patch: hunk.raw });
      toast("Hunk accepted", "ok");
      await loadGitLog();
    } catch (e) {
      toast(e instanceof Error ? e.message : "apply failed", "err");
    }
  };

  const rejectHunk = async (hunk: Hunk) => {
    if (!card.worktreePath) return;
    try {
      await rpc("git.rejectHunk", { path: card.worktreePath, patch: hunk.raw });
      toast("Hunk rejected", "ok");
      await loadGitLog();
    } catch (e) {
      toast(e instanceof Error ? e.message : "reject failed", "err");
    }
  };

  return (
    <div
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,.7)",
        display: "flex", alignItems: "center", justifyContent: "center", zIndex: 100,
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: "var(--bg-raised)", border: "1px solid var(--border)",
          borderRadius: 12, padding: 20, minWidth: 520, maxWidth: 720,
          maxHeight: "80vh", overflow: "auto",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 12 }}>
          <div>
            <h3 style={{ margin: 0 }}>{card.title}</h3>
            <span className="badge" style={{ color: `var(--col-${card.column})` }}>{card.column}</span>
          </div>
          <button style={{ fontSize: 16, background: "none", border: 0 }} onClick={onClose}>✕</button>
        </div>

        {card.description && (
          <p style={{ color: "var(--muted)", fontSize: 12, marginTop: 0 }}>{card.description}</p>
        )}

        {(card.acceptanceCriteria ?? []).length > 0 && (
          <div style={{ marginBottom: 12 }}>
            <div className="section-label" style={{ padding: 0, marginBottom: 4 }}>Acceptance criteria</div>
            {card.acceptanceCriteria!.map((c, i) => (
              <div key={i} style={{ fontSize: 11, padding: "2px 0" }}>□ {c}</div>
            ))}
          </div>
        )}

        {card.gitBranch && (
          <div style={{ marginBottom: 12 }}>
            <div className="section-label" style={{ padding: 0, marginBottom: 4 }}>Git</div>
            <span className="badge">{card.gitBranch}</span>
            {card.worktreePath && (
              <span style={{ marginLeft: 8, fontSize: 10, color: "var(--muted)" }}>{card.worktreePath}</span>
            )}
            {gitStatus && (
              <div style={{ marginTop: 6, fontSize: 11, color: "var(--muted)" }}>
                {gitStatus.dirty ? "dirty" : "clean"}
                {gitStatus.changed?.length ? ` · ${gitStatus.changed.length} changed` : ""}
                {gitStatus.untracked?.length ? ` · ${gitStatus.untracked.length} untracked` : ""}
              </div>
            )}
            {hunks.length > 0 ? (
              <div style={{ marginTop: 8 }}>
                {hunks.map((hunk, i) => (
                  <div key={i} className="hunk-block">
                    <div className="hunk-header">
                      <span>{hunk.header}</span>
                      <span className="hunk-header-actions">
                        <button className="hunk-accept" onClick={() => void applyHunk(hunk)}>✓ Accept</button>
                        <button className="hunk-reject" onClick={() => void rejectHunk(hunk)}>✗ Reject</button>
                      </span>
                    </div>
                    <pre className="hunk-pre">
                      {hunk.lines.map((line, j) => (
                        <span
                          key={j}
                          className={line.startsWith("+") ? "hunk-add" : line.startsWith("-") ? "hunk-del" : "hunk-ctx"}
                        >{line}</span>
                      ))}
                    </pre>
                  </div>
                ))}
              </div>
            ) : gitDiff ? (
              <pre className="git-diff">{gitDiff.slice(0, 8000)}</pre>
            ) : null}
          </div>
        )}

        {card.worktreePath && (
          <div style={{ marginBottom: 12 }}>
            <div className="section-label" style={{ padding: 0, marginBottom: 4 }}>Commit</div>
            <div style={{ display: "flex", gap: 6 }}>
              <input
                style={{ flex: 1 }}
                placeholder="Commit message…"
                value={commitMsg}
                onChange={(e) => setCommitMsg(e.target.value)}
              />
              <button
                style={{ background: "var(--ok)", color: "#000", border: 0 }}
                onClick={commit}
                disabled={!commitMsg}
              >
                Commit
              </button>
            </div>
            {gitLog.length > 0 && (
              <div style={{ marginTop: 6 }}>
                {gitLog.map((c) => (
                  <div key={c.hash} style={{ fontSize: 10, padding: "2px 0", color: "var(--muted)" }}>
                    <span style={{ fontFamily: "var(--mono)", marginRight: 6 }}>{c.hash.slice(0, 7)}</span>
                    <span style={{ color: "var(--text)" }}>{c.subject}</span>
                    <span style={{ marginLeft: 8 }}>{c.author}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {card.worktreePath && (
          <div style={{ marginBottom: 12 }}>
            <div className="section-label" style={{ padding: 0, marginBottom: 4 }}>PR Oluştur</div>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              <input
                placeholder="PR başlığı…"
                value={prTitle}
                onChange={(e) => setPrTitle(e.target.value)}
              />
              <textarea
                placeholder="PR açıklaması (opsiyonel)…"
                value={prBody}
                onChange={(e) => setPrBody(e.target.value)}
                rows={3}
                style={{ resize: "vertical", fontFamily: "var(--mono)", fontSize: 11 }}
              />
              <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
                <label style={{ fontSize: 11, color: "var(--muted)", whiteSpace: "nowrap" }}>Base branch:</label>
                <input
                  style={{ flex: 1 }}
                  placeholder="main"
                  value={prBase}
                  onChange={(e) => setPrBase(e.target.value)}
                />
                <button
                  style={{ background: "var(--accent)", color: "#fff", border: 0, whiteSpace: "nowrap" }}
                  disabled={!prTitle || prLoading}
                  onClick={() => {
                    if (!card.worktreePath || !prTitle) return;
                    setPrLoading(true);
                    void rpc<{ url?: string }>("git.pr", {
                      path: card.worktreePath,
                      title: prTitle,
                      body: prBody,
                      base: prBase || "main",
                    }).then((res) => {
                      const url = res?.url ?? "";
                      toast("PR oluşturuldu", "ok");
                      // Save PR url as artifact
                      void rpc("card.addArtifact", {
                        id: card.id,
                        artifact: { kind: "pr", label: prTitle, url, name: prTitle },
                      });
                      // Move card to review column
                      void rpc("card.move", { id: card.id, column: "review" });
                      setPrTitle("");
                      setPrBody("");
                    }).catch((err) => {
                      toast(err instanceof Error ? err.message : "PR oluşturulamadı", "err");
                    }).finally(() => setPrLoading(false));
                  }}
                >
                  {prLoading ? "…" : "PR Oluştur"}
                </button>
              </div>
            </div>
          </div>
        )}

        <div style={{ marginBottom: 12 }}>
          <div className="section-label" style={{ padding: 0, marginBottom: 4 }}>Artifacts</div>
          {(card.artifacts ?? []).map((a: Artifact, i: number) => (
            <div key={i} style={{ fontSize: 11, padding: "2px 0" }}>
              <span className="badge">{a.kind}</span>
              <span style={{ marginLeft: 6 }}>{a.label}</span>
              {a.url && (
                <a href={a.url} target="_blank" rel="noreferrer" style={{ marginLeft: 6, color: "var(--accent)", fontSize: 10 }}>
                  open
                </a>
              )}
            </div>
          ))}
          <div style={{ display: "flex", gap: 6, marginTop: 6 }}>
            <input style={{ width: 80 }} value={artKind} onChange={(e) => setArtKind(e.target.value)} placeholder="kind" />
            <input style={{ flex: 1 }} value={artLabel} onChange={(e) => setArtLabel(e.target.value)} placeholder="label" />
            <input style={{ flex: 1 }} value={artPath} onChange={(e) => setArtPath(e.target.value)} placeholder="path" />
            <button
              disabled={!artLabel}
              onClick={() => {
                void rpc("card.addArtifact", {
                  id: card.id,
                  artifact: { kind: artKind || "file", label: artLabel, path: artPath, name: artLabel },
                }).then(() => {
                  toast(artLabel, "ok");
                  setArtLabel("");
                  setArtPath("");
                }).catch((err) => toast(err instanceof Error ? err.message : "artifact", "err"));
              }}
            >
              +
            </button>
          </div>
        </div>

        {logs.length > 0 && (
          <div>
            <div className="section-label" style={{ padding: 0, marginBottom: 4 }}>Logs</div>
            <div
              style={{
                background: "var(--bg)", border: "1px solid var(--border)",
                borderRadius: 6, padding: 8, maxHeight: 160, overflow: "auto",
                fontFamily: "var(--mono)", fontSize: 10,
              }}
            >
              {logs.map((l, i) => (
                <div key={i} style={{
                  color: l.level === "error" ? "var(--bad)" : l.level === "warn" ? "var(--warn)" : "var(--muted)",
                  padding: "1px 0",
                }}>
                  <span style={{ opacity: .5, marginRight: 8 }}>{l.source}</span>
                  {l.message}
                </div>
              ))}
            </div>
          </div>
        )}
        <div style={{ marginTop: 12 }}>
          <HarnessPanel
            goalId={card.goalId ?? undefined}
            cardId={String(card.id)}
            collapsible={true}
          />
        </div>
      </div>
    </div>
  );
}