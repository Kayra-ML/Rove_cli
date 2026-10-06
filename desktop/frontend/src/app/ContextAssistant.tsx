import { useCallback, useEffect, useRef, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { Message, Session } from "~/lib/types";
import type { ModelChoice } from "~/lib/slash";
import { Markdown } from "~/lib/markdown";
import { toast } from "~/lib/toast";
import { t, type Lang } from "~/lib/i18n";
import { BRIEF_MARK, contextBrief, pickedLine, referencedItems, splitActions, type CtxItem, type MapAction, type SessionContext } from "~/lib/sessionContext";
import { Icon } from "./Icons";

interface Props {
  // the chat whose context is on the map
  chat: Session;
  ctx: SessionContext;
  model: string;
  win: number;
  picked: CtxItem | null;
  lang: Lang;
  // tucked away: still mounted, so a question asked keeps being answered
  hidden: boolean;
  onHide: () => void;
  // carry out an edit the assistant offered and the user confirmed
  onAction: (a: MapAction) => Promise<void>;
  // working or not, for the launcher while the panel is tucked away
  onBusy?: (busy: boolean) => void;
  // what the reply is talking about, for the map to glide to
  onRefs?: (ids: string[]) => void;
}

type Shown = { id: string; role: "user" | "assistant"; text: string; actions: MapAction[] };

// a cheap fingerprint: the brief goes along again only when it changed
function fingerprint(s: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(36);
}

function toShown(history: Message[]): Shown[] {
  const out: Shown[] = [];
  for (const m of history) {
    if (m.role === "user") {
      const text = m.content.split(BRIEF_MARK)[0].trim();
      if (text) out.push({ id: m.id, role: "user", text, actions: [] });
    } else if (m.role === "assistant" && m.content?.trim()) {
      const { text, actions } = splitActions(m.content);
      out.push({ id: m.id, role: "assistant", text, actions });
    }
  }
  return out;
}

// ContextAssistant is a chat about a chat, beside its context map: ask what
// a request did, what fills the window, which files matter — and take the
// edits it offers (summarize, forget from a request on, rename, point at
// something on the map). It talks in a hidden child of the chat, so the
// conversation is kept, and it gets the chat's context as a brief with the
// first message and whenever that context changes.
export function ContextAssistant({ chat, ctx, model, win, picked, lang, hidden, onHide, onAction, onBusy, onRefs }: Props) {
  const [helper, setHelper] = useState<Session | null>(null);
  const [shown, setShown] = useState<Shown[]>([]);
  const [streaming, setStreaming] = useState("");
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState("");
  const [models, setModels] = useState<ModelChoice[]>([]);
  const [current, setCurrent] = useState("");
  // why the last message did not go through, said in the panel
  const [err, setErr] = useState("");
  // an edit waiting for its second click, and the ones already carried out
  const [arming, setArming] = useState<string | null>(null);
  const [done, setDone] = useState<Set<string>>(new Set());
  const listRef = useRef<HTMLDivElement | null>(null);
  const boxRef = useRef<HTMLTextAreaElement | null>(null);

  // Every change to what is shown — a load, a message sent — takes a turn
  // number; a load that comes back after a newer one started is dropped. An
  // opening load that is slow would otherwise wipe out a question asked
  // meanwhile, and the answer being written with it.
  const turn = useRef(0);
  const reload = useCallback(async (id: string) => {
    const mine = ++turn.current;
    const h = await rpc<Message[]>("session.history", { sessionId: id }).catch(() => []);
    if (mine !== turn.current) return;
    setShown(toShown(h ?? []));
  }, []);

  useEffect(() => {
    let live = true;
    setHelper(null);
    setShown([]);
    rpc<Session>("ctxmap.assistant", { sessionId: chat.id })
      .then(async (s) => {
        if (!live) return;
        setHelper(s);
        await reload(s.id);
        const m = await rpc<{ provider?: string; model?: string; source?: string }>("session.model", { sessionId: s.id }).catch(() => null);
        if (live && m?.source === "chat" && m.model) setCurrent(`${m.provider ?? ""}/${m.model}`);
      })
      .catch((e) => toast(e instanceof Error ? e.message : "assistant failed", "err"));
    rpc<ModelChoice[]>("model.list").then((l) => { if (live) setModels(l ?? []); }).catch(() => {});
    return () => { live = false; };
  }, [chat.id, reload]);

  // the reply as it is written
  useEffect(() => {
    if (!helper) return;
    return subscribeEvents(`session.${helper.id}`, (ev) => {
      const p = (ev.payload ?? {}) as { delta?: string };
      if (ev.type === "message.delta") setStreaming((s) => s + String(p.delta ?? ""));
      if (ev.type === "message.done") {
        setStreaming("");
        void reload(helper.id);
      }
    });
  }, [helper, reload]);

  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [shown, streaming, busy]);

  useEffect(() => { if (!hidden) boxRef.current?.focus(); }, [helper, hidden]);

  useEffect(() => { onBusy?.(busy); }, [busy]); // eslint-disable-line react-hooks/exhaustive-deps

  // As the reply comes in, and once it is done, the map follows what it
  // talks about: a few times a second at most, and only when that changes.
  const lastRefs = useRef("");
  const follow = useCallback((text: string, actions: MapAction[] = []) => {
    const ids = referencedItems(ctx, text);
    for (const a of actions) {
      if (a.action === "show" && a.turn) ids.push(`p:${a.turn - 1}`);
    }
    const key = [...new Set(ids)].sort().join("|");
    if (!key || key === lastRefs.current) return;
    lastRefs.current = key;
    onRefs?.([...new Set(ids)]);
  }, [ctx, onRefs]);
  useEffect(() => {
    if (!streaming) return;
    const id = window.setTimeout(() => follow(streaming), 700);
    return () => window.clearTimeout(id);
  }, [streaming, follow]);
  const lastReply = [...shown].reverse().find((m) => m.role === "assistant");
  useEffect(() => {
    if (lastReply && !busy) follow(lastReply.text, lastReply.actions);
  }, [lastReply?.id, busy]); // eslint-disable-line react-hooks/exhaustive-deps

  const send = async (raw: string) => {
    const text = raw.trim();
    if (!text || !helper || busy) return;
    const brief = contextBrief(ctx, chat.title, model, win, picked);
    const key = `aether.ctx.brief.${helper.id}`;
    const fp = fingerprint(brief);
    let last = "";
    try { last = localStorage.getItem(key) ?? ""; } catch { /* private */ }
    const content = fp !== last
      ? `${text}${BRIEF_MARK}\n${brief}`
      : picked ? `${text}${BRIEF_MARK} (bağlam değişmedi)\nHaritada seçili: ${pickedLine(ctx, picked)}` : text;
    setInput("");
    setErr("");
    lastRefs.current = "";
    setBusy(true);
    setStreaming("");
    turn.current++;
    setShown((s) => [...s, { id: `local-${Date.now()}`, role: "user", text, actions: [] }]);
    try {
      await rpc("session.send", { sessionId: helper.id, agentId: helper.agentId, workspaceId: chat.workspaceId, content });
      try { localStorage.setItem(key, fp); } catch { /* private */ }
    } catch (e) {
      setErr(e instanceof Error ? e.message : "send failed");
      setShown((s) => s.filter((m) => !m.id.startsWith("local-")));
      setInput(text);
    } finally {
      setBusy(false);
      setStreaming("");
      await reload(helper.id);
    }
  };

  const pickModel = async (value: string) => {
    if (!helper) return;
    const [provider, ...rest] = value.split("/");
    const m = value ? { provider, model: rest.join("/") } : { provider: "", model: "" };
    try {
      await rpc("session.setModel", { sessionId: helper.id, ...m });
      setCurrent(value);
      setErr("");
    } catch (e) {
      toast(e instanceof Error ? e.message : "model failed", "err");
    }
  };

  const fresh = async () => {
    if (!helper || busy) return;
    await rpc("session.truncate", { sessionId: helper.id, keep: 0 }).catch(() => {});
    try { localStorage.removeItem(`aether.ctx.brief.${helper.id}`); } catch { /* private */ }
    setDone(new Set());
    await reload(helper.id);
  };

  const run = async (key: string, a: MapAction) => {
    // what cannot be undone asks twice
    if ((a.action === "forget" || a.action === "compact") && arming !== key) {
      setArming(key);
      return;
    }
    setArming(null);
    try {
      await onAction(a);
      if (a.action !== "show") setDone((d) => new Set(d).add(key));
    } catch (e) {
      toast(e instanceof Error ? e.message : "failed", "err");
    }
  };

  const label = (a: MapAction) => {
    switch (a.action) {
      case "compact": return t("caCompact", lang);
      case "forget": return `${a.fromTurn}. ${t("caForget", lang)}`;
      case "rename": return `${t("caRename", lang)}: "${a.title}"`;
      case "show": return `${t("caShow", lang)}: ${a.turn ? `${a.turn}. ${t("ctxTurns", lang)}` : a.file?.split("/").pop()}`;
    }
  };

  const hints = [t("caHint1", lang), t("caHint2", lang), t("caHint3", lang), t("caHint4", lang)];
  const modelKeys = models.filter((m) => m.model).map((m) => `${m.provider}/${m.model}`);

  return (
    <aside className={`ca${hidden ? " is-hidden" : ""}`} aria-label={t("caTitle", lang)} aria-hidden={hidden}>
      <header className="ca-head">
        <div className="ca-title"><Icon name="lightning" size={13} /> {t("caTitle", lang)}</div>
        <select className="ca-model" value={current} onChange={(e) => void pickModel(e.target.value)} aria-label={t("caModel", lang)} title={t("caModel", lang)}>
          <option value="">{t("caModelDefault", lang)}</option>
          {current && !modelKeys.includes(current) && <option value={current}>{current.split("/").slice(1).join("/")}</option>}
          {models.filter((m) => m.model).map((m) => (
            <option key={`${m.provider}/${m.model}`} value={`${m.provider}/${m.model}`}>{m.model}</option>
          ))}
        </select>
        <button type="button" className="icon-btn" title={t("caNew", lang)} aria-label={t("caNew", lang)} onClick={() => void fresh()} disabled={busy || shown.length === 0}>↺</button>
        <button type="button" className="icon-btn" title={t("caHide", lang)} aria-label={t("caHide", lang)} onClick={onHide}>⌄</button>
      </header>

      <div className="ca-list" ref={listRef}>
        {shown.length === 0 && !busy && (
          <div className="ca-empty">
            <p>{t("caIntro", lang)}</p>
            <div className="ca-hints">
              {picked && picked.kind !== "session" && (
                <button type="button" onClick={() => void send(`${t("caAboutPicked", lang)}: ${picked.label}`)}>{t("caAboutPicked", lang)}: {picked.label}</button>
              )}
              {hints.map((h) => <button key={h} type="button" onClick={() => void send(h)}>{h}</button>)}
            </div>
          </div>
        )}
        {shown.map((m) => (
          <div key={m.id} className={`ca-msg ${m.role}`}>
            {m.role === "user" ? <div className="ca-bubble">{m.text}</div> : (
              <>
                {m.text && <div className="ca-reply"><Markdown text={m.text} /></div>}
                {m.actions.length > 0 && (
                  <div className="ca-actions">
                    {m.actions.map((a, i) => {
                      const key = `${m.id}:${i}`;
                      const isDone = done.has(key);
                      return (
                        <button key={key} type="button" disabled={isDone}
                          className={`ca-act ${a.action}${arming === key ? " arming" : ""}${isDone ? " done" : ""}`}
                          onClick={() => void run(key, a)}>
                          {isDone ? `✓ ${label(a)}` : arming === key ? `${t("caSure", lang)} — ${label(a)}` : label(a)}
                        </button>
                      );
                    })}
                  </div>
                )}
              </>
            )}
          </div>
        ))}
        {busy && (
          <div className="ca-msg assistant">
            {streaming ? <div className="ca-reply"><Markdown text={splitActions(streaming).text} /></div> : <div className="ca-typing"><i /><i /><i /></div>}
          </div>
        )}
      </div>

      <form className="ca-form" onSubmit={(e) => { e.preventDefault(); void send(input); }}>
        {err && <div className="ca-err" role="alert">{t("caFailed", lang)}: {err}</div>}
        {picked && picked.kind !== "session" && <div className="ca-picked">{t("caPicked", lang)}: <b>{picked.label}</b></div>}
        <div className="ca-row">
          <textarea
            ref={boxRef}
            rows={2}
            value={input}
            placeholder={t("caPlaceholder", lang)}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(input); }
              if (e.key === "Escape") onHide();
            }}
          />
          <button type="submit" className="send-btn" aria-label={t("send", lang)} disabled={!input.trim() || busy || !helper}>↑</button>
        </div>
      </form>
    </aside>
  );
}
