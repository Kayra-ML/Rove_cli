import { useCallback, useEffect, useRef, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { Agent, Message, Session } from "~/lib/types";
import { useAgents, useHistory, useSessions } from "~/hooks/useApi";
import { usePrefs } from "~/hooks/usePrefs";
import { useVoice } from "~/hooks/useVoice";
import { t } from "~/lib/i18n";
import { Markdown } from "~/lib/markdown";
import { filterSlash, lastUserKeep, matchSlash, SLASH, type SlashCmd, type SlashId } from "~/lib/slash";
import type { Card } from "~/lib/types";
import { toast } from "~/lib/toast";

interface Props {
  workspaceId?: string;
  session?: Session | null;
  onSession?: (s: Session) => void;
  onActivity?: (map: Record<string, string>) => void;
  onOpenBoard?: () => void;
  onSelectCard?: (card: Card) => void;
}

export function Chat({ workspaceId, session, onSession, onActivity, onOpenBoard, onSelectCard }: Props) {
  const { lang } = usePrefs();
  const { agents } = useAgents();
  const [activeAgent, setActiveAgent] = useState<Agent | null>(null);
  const { sessions, reload: reloadSessions } = useSessions(workspaceId);
  const [activeSession, setActiveSession] = useState<Session | null>(session ?? null);
  const { messages, reload: reloadHistory } = useHistory(activeSession?.id ?? null);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [streaming, setStreaming] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [attachOpen, setAttachOpen] = useState(false);
  const [activity, setActivity] = useState<Record<string, string>>({});
  const [sendErr, setSendErr] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [slashHi, setSlashHi] = useState(0);
  const undoStack = useRef<Message[][]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);
  const pickerRef = useRef<HTMLDivElement>(null);
  const attachRef = useRef<HTMLDivElement>(null);
  const taRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const streamFor = useRef<string | null>(null);

  const handleTranscript = useCallback((text: string) => {
    setInput((cur) => cur ? `${cur} ${text}` : text);
  }, []);
  const { listening, supported: voiceSupported, start: startVoice, stop: stopVoice } = useVoice(handleTranscript);

  useEffect(() => {
    if (session && session.id !== activeSession?.id) setActiveSession(session);
  }, [session]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (agents.length > 0 && !activeAgent) setActiveAgent(agents[0]);
  }, [agents, activeAgent]);

  useEffect(() => {
    if (!activeSession) return;
    const sid = activeSession.id;
    streamFor.current = sid;
    setStreaming("");
    const unsub = subscribeEvents(`session.${sid}`, (ev) => {
      if (streamFor.current !== sid) return;
      if (ev.type === "message.delta") {
        const delta = (ev.payload as Record<string, string>)?.delta ?? "";
        setStreaming((s) => s + delta);
        if (activeAgent) setActivity((a) => ({ ...a, [activeAgent.id]: t("writing", lang) }));
      }
      if (ev.type === "tool.start") {
        const name = (ev.payload as Record<string, string>)?.name ?? "tool";
        if (activeAgent) setActivity((a) => ({ ...a, [activeAgent.id]: name }));
      }
      if (ev.type === "message.done") {
        setStreaming("");
        if (activeAgent) setActivity((a) => ({ ...a, [activeAgent.id]: "" }));
        void reloadHistory();
      }
    });
    return () => {
      unsub();
      if (streamFor.current === sid) streamFor.current = null;
    };
  }, [activeSession, reloadHistory, activeAgent, lang]);

  useEffect(() => {
    onActivity?.(activity);
  }, [activity, onActivity]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streaming]);

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      const n = e.target as Node;
      if (pickerRef.current && !pickerRef.current.contains(n)) setPickerOpen(false);
      if (attachRef.current && !attachRef.current.contains(n)) setAttachOpen(false);
    }
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  useEffect(() => {
    const el = taRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [input]);

  const switchAgent = useCallback(async (agent: Agent) => {
    setActiveAgent(agent);
    setPickerOpen(false);
    setStreaming("");
    const mine = sessions.filter((s) => s.agentId === agent.id);
    if (mine.length > 0) {
      setActiveSession(mine[0]);
      onSession?.(mine[0]);
      return;
    }
    const sess = await rpc<Session>("session.create", {
      title: agent.name,
      agentId: agent.id,
      workspaceId: workspaceId ?? "",
    });
    await reloadSessions();
    setActiveSession(sess);
    onSession?.(sess);
  }, [sessions, workspaceId, reloadSessions, onSession]);

  async function newSession() {
    if (!activeAgent) return;
    const sess = await rpc<Session>("session.create", {
      title: t("chat", lang),
      agentId: activeAgent.id,
      workspaceId: workspaceId ?? "",
    });
    await reloadSessions();
    setActiveSession(sess);
    onSession?.(sess);
  }

  const slashHits = input.startsWith("/") ? filterSlash(input) : [];

  function hintFor(id: SlashId): string {
    const keys: Record<SlashId, string> = {
      new: "slashNew", undo: "slashUndo", redo: "slashRedo",
      review: "slashReview", stop: "slashStop", run: "slashRun",
      clear: "slashClear", help: "slashHelp",
    };
    return t(keys[id], lang);
  }

  async function applyKeep(keep: number) {
    if (!activeSession) return;
    await rpc("session.truncate", { sessionId: activeSession.id, keep });
    await reloadHistory();
  }

  async function runSlash(id: SlashId, rest: string) {
    setInput("");
    setSendErr("");
    switch (id) {
      case "new":
        undoStack.current = [];
        await newSession();
        return;
      case "stop":
        await stop();
        return;
      case "run": {
        const out = await rpc<{ lastResult?: string }[]>("automation.tick");
        const n = Array.isArray(out) ? out.length : 0;
        toast(n ? `tick ${n}` : "tick", "ok");
        onOpenBoard?.();
        return;
      }
      case "clear": {
        if (!activeSession) return;
        undoStack.current.push(messages);
        await applyKeep(0);
        return;
      }
      case "undo": {
        if (!activeSession) return;
        const keep = lastUserKeep(messages);
        if (keep >= messages.length) {
          setSendErr("undo: nothing to rewind");
          return;
        }
        undoStack.current.push(messages.slice(keep));
        await applyKeep(keep);
        // If a workspace is active, also restore the most recent file snapshot.
        if (workspaceId) {
          try {
            type Snap = { ref: string };
            const snaps = await rpc<Snap[]>("checkpoint.list", { path: workspaceId });
            if (Array.isArray(snaps) && snaps.length > 0) {
              await rpc("checkpoint.restore", { path: workspaceId, ref: snaps[0].ref });
              toast("Workspace snapshot restored", "ok");
            }
          } catch {
            // checkpoint restore is best-effort; don't block undo
          }
        }
        return;
      }
      case "redo": {
        if (!activeSession) return;
        const chunk = undoStack.current.pop();
        if (!chunk?.length) {
          setSendErr("redo: empty");
          return;
        }
        await rpc("session.append", {
          sessionId: activeSession.id,
          messages: chunk.map((m) => ({
            sessionId: activeSession.id,
            role: m.role,
            content: m.content,
          })),
        });
        await reloadHistory();
        return;
      }
      case "review": {
        const last = [...messages].reverse().find((m) => m.role === "assistant");
        const title = (rest || last?.content.split("\n")[0] || "Review").slice(0, 80);
        const card = await rpc<Card>("card.create", {
          title,
          column: "review",
          workspaceId: workspaceId ?? "",
          description: last?.content ?? rest,
          sessionId: activeSession?.id ?? "",
        });
        onSelectCard?.(card);
        onOpenBoard?.();
        toast(title, "ok");
        return;
      }
      case "help":
        setSendErr(SLASH.map((c) => `/${c.id} — ${hintFor(c.id)}`).join(" · "));
        return;
    }
  }

  async function send() {
    const msg = input.trim();
    const hit = matchSlash(msg);
    if (hit) {
      try {
        await runSlash(hit.cmd.id, hit.rest);
      } catch (err) {
        const msg = err instanceof Error ? err.message : "command failed";
        setSendErr(msg);
        toast(msg, "err");
      }
      return;
    }
    if (!msg || !activeSession || !activeAgent || sending) return;

    // @codebase mention: search the codebase index and prepend results as context.
    let content = msg;
    if (msg.includes("@codebase")) {
      const query = msg.replace(/@codebase/g, "").trim();
      try {
        type IndexResult = { path: string; snippet: string };
        const results = await rpc<IndexResult[]>("index.search", {
          query: query || msg,
          limit: 8,
        });
        if (results && results.length > 0) {
          const ctx = results
            .map((r: IndexResult) => `### ${r.path}\n${r.snippet}`)
            .join("\n\n");
          content = `<codebase-context>\n${ctx}\n</codebase-context>\n\n${msg}`;
        }
      } catch {
        // index unavailable — send without context
      }
    }

    setInput("");
    setSending(true);
    setSendErr("");
    setActivity((a) => ({ ...a, [activeAgent.id]: t("thinking", lang) }));
    try {
      await rpc("session.send", {
        sessionId: activeSession.id,
        agentId: activeAgent.id,
        workspaceId: workspaceId ?? "",
        content,
      });
      await reloadHistory();
    } catch (err) {
      setInput(msg);
      const fail = err instanceof Error ? err.message : "send failed";
      setSendErr(fail);
      toast(fail, "err");
      setActivity((a) => ({ ...a, [activeAgent.id]: "" }));
    } finally {
      setSending(false);
    }
  }

  async function stop() {
    if (!activeAgent) return;
    await rpc("agent.cancel", { agentId: activeAgent.id }).catch(() => {});
    setSending(false);
    setStreaming("");
    setActivity((a) => ({ ...a, [activeAgent.id]: "" }));
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (slashHits.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSlashHi((i) => (i + 1) % slashHits.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSlashHi((i) => (i - 1 + slashHits.length) % slashHits.length);
        return;
      }
      if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !matchSlash(input.trim()))) {
        e.preventDefault();
        const pick = slashHits[slashHi] ?? slashHits[0];
        setInput(`/${pick.id} `);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setInput("");
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  }

  async function attachFiles(files: FileList | File[]) {
    const list = Array.from(files);
    if (!list.length) return;
    const bits = await Promise.all(list.map(async (f) => {
      const text = await f.text().catch(() => "");
      const body = text.slice(0, 12_000);
      return `### ${f.name}\n\`\`\`\n${body}\n\`\`\``;
    }));
    setInput((cur) => (cur ? `${cur}\n\n${bits.join("\n\n")}` : bits.join("\n\n")));
  }

  type StreamMsg = { id: string; role: string; content: string; streaming: true };
  const allMessages: (Message | StreamMsg)[] = streaming
    ? [...messages, { id: "__stream__", role: "assistant", content: streaming, streaming: true }]
    : messages;

  const live = activeAgent ? activity[activeAgent.id] : "";
  const canSend = Boolean(input.trim() && activeSession && !sending);
  const empty = allMessages.length === 0;

  async function exportSession() {
    if (!activeSession) return;
    const res = await rpc<{ markdown: string; filename: string }>("session.export", {
      sessionId: activeSession.id,
    });
    const blob = new Blob([res.markdown], { type: "text/markdown" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = res.filename;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div
      className="thread"
      onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        if (e.dataTransfer.files.length) void attachFiles(e.dataTransfer.files);
      }}
    >
      <div className="thread-viewport">
        {empty ? (
          <div className="intro">
            <p className="wordmark" aria-label="AETHER"><span>AETHER</span></p>
            <p className="intro-body">{t("introBody", lang)}</p>
          </div>
        ) : (
          <div className="transcript">
            {allMessages.map((m) => {
              const isUser = m.role === "user";
              const streamingMsg = "streaming" in m;
              return (
                <div key={m.id} className={isUser ? "turn turn-user" : "turn turn-assistant"} data-role={m.role}>
                  {isUser ? (
                    <div className="user-bubble"><Markdown text={m.content} /></div>
                  ) : (
                    <div className={`assistant-copy${streamingMsg ? " streaming" : ""}`}>
                      {m.content ? <Markdown text={m.content} /> : (streamingMsg ? "▍" : "")}
                    </div>
                  )}
                  {!streamingMsg && m.content && (
                    <button
                      type="button"
                      className="ghost copy-msg"
                      onClick={() => void navigator.clipboard.writeText(m.content)}
                      aria-label={t("copy", lang)}
                    >
                      {t("copy", lang)}
                    </button>
                  )}
                </div>
              );
            })}
            <div ref={bottomRef} />
          </div>
        )}
      </div>

      {activeSession && (
        <div style={{ display: "flex", justifyContent: "flex-end", padding: "0 12px 4px" }}>
          <button
            type="button"
            className="ghost"
            style={{ fontSize: 11, opacity: 0.6 }}
            onClick={() => void exportSession()}
            aria-label="Export session as markdown"
          >
            ↓ export .md
          </button>
        </div>
      )}
      <div className="composer-dock">
        {sendErr && <div className="status-stack" style={{ color: "var(--bad)" }}>{sendErr}</div>}
        {(live || sending) && (
          <div className="status-stack">
            <span className="pip run" />
            <span>{activeAgent?.name ?? "agent"} · {live || t("thinking", lang)}</span>
            <button type="button" className="ghost" style={{ marginLeft: "auto", fontSize: 11 }} onClick={() => void stop()}>
              {t("stop", lang)}
            </button>
          </div>
        )}
        {dragOver && <div className="status-stack">{t("dropFiles", lang)}</div>}

        <form className="composer-root" data-slot="composer-root" onSubmit={(e) => { e.preventDefault(); void send(); }}>
          {slashHits.length > 0 && (
            <div className="slash-menu" role="listbox">
              {slashHits.map((c: SlashCmd, i) => (
                <button
                  type="button"
                  key={c.id}
                  className={`slash-row${i === slashHi ? " active" : ""}`}
                  onMouseEnter={() => setSlashHi(i)}
                  onClick={() => { setInput(`/${c.id} `); taRef.current?.focus(); }}
                >
                  <code>/{c.id}</code>
                  <span>{hintFor(c.id)}</span>
                </button>
              ))}
            </div>
          )}
          {attachOpen && (
            <div className="composer-panel composer-panel-left" ref={attachRef}>
              <div className="section-label" style={{ paddingTop: 8 }}>{t("sessions", lang)}</div>
              <button type="button" className="profile-row" onClick={() => { setAttachOpen(false); void newSession(); }}>+ {t("chat", lang)}</button>
              <button type="button" className="profile-row" onClick={() => { setAttachOpen(false); fileRef.current?.click(); }}>{t("dropFiles", lang)}</button>
            </div>
          )}
          {pickerOpen && (
            <div className="profile-menu composer-panel-right" ref={pickerRef}>
              <div className="section-label" style={{ paddingTop: 8 }}>{t("subagents", lang)}</div>
              {agents.length === 0 && (
                <div style={{ padding: "8px 12px", color: "var(--faint)", fontSize: 12 }}>{t("noneYet", lang)}</div>
              )}
              {agents.map((a) => {
                const doing = activity[a.id];
                return (
                  <button
                    type="button"
                    key={a.id}
                    className={`profile-row${activeAgent?.id === a.id ? " active" : ""}`}
                    onClick={() => void switchAgent(a)}
                  >
                    <span className={`pip${a.status === "running" || doing ? " run" : " on"}`} />
                    <span className="meta">
                      <strong>{a.name}</strong>
                      <span>{doing || a.model}</span>
                    </span>
                  </button>
                );
              })}
            </div>
          )}
          <input
            ref={fileRef}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              if (e.target.files) void attachFiles(e.target.files);
              e.target.value = "";
            }}
          />
          <div className="composer-surface" data-slot="composer-surface">
            <div className="composer-fade">
              <div className="composer-row">
                <div className="composer-menu">
                  <button
                    type="button"
                    className="icon-ghost"
                    aria-label="+"
                    onClick={() => { setPickerOpen(false); setAttachOpen((o) => !o); }}
                  >
                    +
                  </button>
                  {voiceSupported && (
                    <button
                      type="button"
                      className={`icon-ghost mic-btn${listening ? " listening" : ""}`}
                      aria-label={listening ? "Stop recording" : "Start voice input"}
                      onClick={() => listening ? stopVoice() : startVoice()}
                    >
                      🎙
                    </button>
                  )}
                </div>
                <textarea
                  ref={taRef}
                  className="composer-input"
                  rows={1}
                  value={input}
                  onChange={(e) => { setInput(e.target.value); setSlashHi(0); }}
                  onKeyDown={handleKeyDown}
                  placeholder={activeAgent ? t("message", lang) : t("pickProfile", lang)}
                  disabled={!activeSession}
                />
                <div className="composer-controls">
                  <div className="composer-profile">
                    <button
                      type="button"
                      className="model-pill"
                      onClick={() => { setAttachOpen(false); setPickerOpen((o) => !o); }}
                    >
                      <span className={`pip${activeAgent?.status === "running" || live ? " run" : activeAgent ? " on" : ""}`} />
                      <span className="pill-label">{activeAgent?.name ?? "model"}</span>
                      <span className="profile-caret">▾</span>
                    </button>
                  </div>
                  {sending ? (
                    <button type="button" className="send-btn" onClick={() => void stop()} aria-label={t("stop", lang)}>■</button>
                  ) : (
                    <button type="submit" className="send-btn" disabled={!canSend} aria-label="Send">↑</button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}
