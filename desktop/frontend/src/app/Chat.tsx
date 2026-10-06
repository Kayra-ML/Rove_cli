import { Fragment, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { Agent, Message, Session } from "~/lib/types";
import { useAgents, useHistory, useProfiles, useSessions } from "~/hooks/useApi";
import { usePrefs } from "~/hooks/usePrefs";
import { useVoice } from "~/hooks/useVoice";
import { t } from "~/lib/i18n";
import { Markdown } from "~/lib/markdown";
import { filterModels, filterSessions, filterSlash, lastUserKeep, matchSlash, paneRef, parseGoal, pickModel, SLASH, type ModelChoice, type SlashCmd, type SlashId, type SlashScope } from "~/lib/slash";
import { toast } from "~/lib/toast";
import wordmarkLogo from "~/assets/logo-256.png";
import { Thinking } from "./Thinking";
import { useCharacterName, usePersona, usePersonaBadges } from "~/hooks/usePersona";
import { spaceOf } from "~/lib/spaces";
import { ago } from "./SessionSidebar";
import { filterFiles, imageToDataUrl, isImage, MAX_IMAGES, mentionAt, mentionedPaths, splitContext, withFiles, type ImageAttachment } from "~/lib/attachments";
import { ChangesBar, ReviewSheet, useEdits } from "./Review";
import { compact } from "./UsageCard";
import type { MapHit } from "~/lib/types";
import { describeTool, estimateTokens, toBlocks, type Block, type ToolOutcome } from "~/lib/transcript";
import { TermTranscript } from "./TermTranscript";
import { Avatar } from "./Avatar";
import { AgentMark } from "./AgentMark";
import { ToolRows } from "./ToolRows";
import { Icon } from "./Icons";

// the model a chat runs on, and where it comes from
type ChatModel = { provider: string; model: string; source: "chat" | "profile" | "agent"; effort?: string };

// reasoning effort levels; "" leaves it to the model
const EFFORTS = ["", "low", "medium", "high"] as const;

// PIN_CLIP is how much of a prompt the pinned line carries; the rest is a
// tooltip. One line is the point of it.
const PIN_CLIP = 160;

// What the model button shows when no provider offers the chat's model: a
// mark that takes the space without naming something that is not there.
const NO_MODEL = "·";

// firstLine is a prompt as the pinned line shows it: its first line, cut
// short if it runs on.
function firstLine(text: string): string {
  const t = text.trim().split("\n").find((l) => l.trim() !== "")?.trim() ?? "";
  return t.length > PIN_CLIP ? `${t.slice(0, PIN_CLIP).trimEnd()}…` : t;
}
// UserPlate is what you asked, as a plate. A prompt pasted in from
// somewhere else can run to dozens of lines, and left alone it pushes the
// answer you came for off the screen — so a tall one is cut to a readable
// height with a way to open it back up. The measuring is done after layout
// because the height depends on wrapping, which only the browser knows.
const PLATE_MAX = 220;

function UserPlate({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [tall, setTall] = useState(false);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    setTall(el.scrollHeight > PLATE_MAX + 24);
  }, [children]);
  return (
    <div className={`user-bubble${tall && !open ? " clipped" : ""}`} ref={ref}>
      {children}
      {tall && (
        <button type="button" className="plate-more" onClick={() => setOpen((v) => !v)}>
          {open ? t("toolLess") : t("plateMore")}
        </button>
      )}
    </div>
  );
}

type Queued = { id: string; text: string; images: ImageAttachment[] };

interface Props {
  workspaceId?: string;
  session?: Session | null;
  onSession?: (s: Session) => void;
  onActivity?: (map: Record<string, string>) => void;
  onOpenMap?: () => void;
  onOpenContext?: () => void;
  // "agent" is the chat view; "terminal" draws the same session as a
  // coding CLI transcript. Logic (send, slash, persona) is shared.
  variant?: "agent" | "terminal";
  onFocusPane?: () => void;
  workspacePath?: string;
  // A new chat that does not exist yet: the first send creates it through
  // onCreateSession, so it only reaches the session list with a message.
  draft?: boolean;
  onCreateSession?: () => Promise<Session>;
  // /new and "+ chat" open a draft instead of creating an empty session.
  onNewDraft?: () => void;
  // set when this chat is one terminal of a multi-terminal session: its
  // label (T2) and the session it belongs to
  pane?: { label: string; groupId: string };
  // Orchestra: the open subagent channel's bar, over the message box in place of the agent chip
  team?: ReactNode;
  // the chat's subagents at work, over everything else in the message box
  dock?: ReactNode;
}

const APP_VERSION = "0.2.10";


export function Chat({ workspaceId, session, onSession, onActivity, onOpenMap, onOpenContext, variant = "agent", onFocusPane, workspacePath, draft, onCreateSession, onNewDraft, pane, team, dock }: Props) {
  // commands that apply here: the terminal's, or the chat's
  const scope: SlashScope = pane ? "terminal" : "chat";
  const { lang } = usePrefs();
  const { agents } = useAgents();
  const [activeAgent, setActiveAgent] = useState<Agent | null>(null);
  const { sessions, reload: reloadSessions } = useSessions(workspaceId);
  const [activeSession, setActiveSession] = useState<Session | null>(session ?? null);
  const { messages, reload: reloadHistory } = useHistory(activeSession?.id ?? null);
  const activeIdRef = useRef<string | null>(null);
  activeIdRef.current = activeSession?.id ?? null;
  const markBusy = useCallback((id: string, on: boolean) => {
    setBusyIn((s) => {
      if (s.has(id) === on) return s;
      const next = new Set(s);
      if (on) next.add(id); else next.delete(id);
      return next;
    });
  }, []);
  // send() can outlive the session it started with (a draft becomes a real
  // session mid-send), so it reloads through whatever is current.
  const reloadRef = useRef(reloadHistory);
  reloadRef.current = reloadHistory;
  // A finished reply asks for the history from more than one place at once
  // (the send returning, the message landing). Over a tunnel each ask is a
  // round trip carrying the whole conversation, so they are collapsed into
  // one — the last one, which sees everything.
  const historySoon = useRef<number | null>(null);
  const reloadHistorySoon = useCallback(() => {
    if (historySoon.current !== null) window.clearTimeout(historySoon.current);
    historySoon.current = window.setTimeout(() => {
      historySoon.current = null;
      void reloadRef.current();
    }, 120);
  }, []);
  useEffect(() => () => {
    if (historySoon.current !== null) window.clearTimeout(historySoon.current);
  }, []);
  const [input, setInput] = useState("");
  // Run state is per session: a reply still running in one chat must not
  // show up as "thinking" in another (or in a new draft).
  const [busyIn, setBusyIn] = useState<Set<string>>(new Set());
  const [creating, setCreating] = useState(false);
  const sending = creating || (activeSession ? busyIn.has(activeSession.id) : false);
  const [streaming, setStreaming] = useState("");
  const [attachOpen, setAttachOpen] = useState(false);
  const [activity, setActivity] = useState<Record<string, string>>({}); // session id → what it is doing
  const [sendErr, setSendErr] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [slashHi, setSlashHi] = useState(0);
  // /models: the open picker (null when closed), what the chat runs on, and
  // a draft's pick, applied once its first message creates the chat
  const [models, setModels] = useState<ModelChoice[] | null>(null);
  const [chatModel, setChatModel] = useState<ChatModel | null>(null);
  // every model the providers actually offer, so the button never names one
  // that is not there (an agent's stored default outlives its provider)
  const [modelCatalog, setModelCatalog] = useState<ModelChoice[] | null>(null);
  const pendingModel = useRef<ModelChoice | null>(null);
  // the picker opened from the model button sits by it; the message being
  // typed is put aside while its box filters models, then given back
  const [modelsFromPill, setModelsFromPill] = useState(false);
  const [modelQuery, setModelQuery] = useState("");
  const stashed = useRef<string | null>(null);
  // /session: the open list of chats to switch to (null when closed)
  const [pickSessions, setPickSessions] = useState<Session[] | null>(null);
  // /character: the open list of roles; a draft's pick until it exists
  // a note shown above the box (/panes)
  const [note, setNote] = useState("");
  // images to send with the next message, and messages written while the
  // agent is still answering (sent in turn, or right away with "now")
  const [images, setImages] = useState<ImageAttachment[]>([]);
  const [queue, setQueue] = useState<Queued[]>([]);
  // @file mentions: the workspace's files (loaded on the first @) and the
  // mention being typed
  const [files, setFiles] = useState<string[] | null>(null);
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null);
  const [mentionHi, setMentionHi] = useState(0);
  // reasoning effort for this chat, and a draft's pick until it exists
  const [effort, setEffortState] = useState("");
  const pendingEffort = useRef<string | null>(null);
  // the project's rule files (AGENTS.md…) the agent reads
  const [rules, setRules] = useState<string[]>([]);
  // the last turn's changes, and the review window
  const { view: edits, setView: setEdits, reload: reloadEdits } = useEdits(activeSession?.id);
  const [reviewOpen, setReviewOpen] = useState(false);
  const { badgeFor } = usePersonaBadges();
  const pillRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const undoStack = useRef<Message[][]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<HTMLDivElement>(null);
  const threadRef = useRef<HTMLDivElement>(null);
  // Following the answer only holds while you are at the foot of it. Scroll
  // up to read something and the view stays where you put it, however much
  // the agent writes; come back to the bottom and it follows again.
  const stickRef = useRef(true);
  const lastTopRef = useRef(0);
  // blank room kept under the last question; see the measuring effect below
  const [tail, setTail] = useState(0);
  // What you just asked, held here until the daemon hands it back in the
  // history. The round trip can take seconds on a slow link, and without
  // this the chat shows nothing at all in the meantime — the message you
  // pressed Enter on is simply not on screen yet.
  const [pendingAsk, setPendingAsk] = useState<{ text: string; images: string[] } | null>(null);
  // where the transcript sits inside the thread, so the pinned line lands
  // exactly over the turns instead of beside them
  const [pinBox, setPinBox] = useState<{ left: number; width: number } | null>(null);
  // the prompt whose answer you are reading: the last one scrolled off the
  // top, pinned there so you can see what the text below is answering
  const [pinned, setPinned] = useState("");
  const attachRef = useRef<HTMLDivElement>(null);
  const taRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const streamFor = useRef<string | null>(null);
  const { view: persona } = usePersona(activeSession?.id);
  const charName = useCharacterName();
  const { profiles } = useProfiles();
  // an office agent: its own name and logo
  const agentProfile = persona?.persona.profileId ? profiles.find((p) => p.id === persona.persona.profileId) ?? null : null;
  // the chat's agent, in the app's language (an office agent keeps the name it was given)
  const agentName = agentProfile ? agentProfile.name : persona ? charName(persona.persona.characterId, persona.persona.name ?? "") : "";
  const agentBadge = agentProfile
    ? <AgentMark mark={agentProfile.mark} color={agentProfile.color} seed={agentProfile.id} character={agentProfile.characterId} size={16} />
    : <Avatar name={agentName} size="xs" accent />;
  // live run state, for runs started here or anywhere else (relays, panes)
  const [liveBlocks, setLive] = useState<Block[]>([]);

  // the agent is working: it has been asked, or it is answering, or a tool of
  // its is running somewhere
  const busy = sending || Boolean(streaming) || liveBlocks.length > 0;
  const [startedAt, setStartedAt] = useState<number | null>(null);
  const [streamChars, setStreamChars] = useState(0);

  const handleTranscript = useCallback((text: string) => {
    setInput((cur) => cur ? `${cur} ${text}` : text);
  }, []);
  const { listening, supported: voiceSupported, start: startVoice, stop: stopVoice } = useVoice(handleTranscript);

  useEffect(() => {
    if (session) {
      if (session.id !== activeSession?.id) setActiveSession(session);
    } else if (draft) {
      setActiveSession(null);
    }
  }, [session, draft]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (agents.length > 0 && !activeAgent) setActiveAgent(agents[0]);
  }, [agents, activeAgent]);

  useEffect(() => {
    // whatever was live belongs to the previous chat, not this one
    setStreaming("");
    setLive([]);
    setStartedAt(null);
    setStreamChars(0);
    if (!activeSession) return;
    const sid = activeSession.id;
    streamFor.current = sid;
    let seq = 0;
    const unsub = subscribeEvents(`session.${sid}`, (ev) => {
      if (streamFor.current !== sid) return;
      const p = (ev.payload ?? {}) as Record<string, unknown>;
      if (ev.type === "message.delta") {
        const delta = String(p.delta ?? "");
        setStreaming((s) => s + delta);
        setStreamChars((n) => n + delta.length);
        setStartedAt((v) => v ?? Date.now());
        setActivity((a) => ({ ...a, [sid]: t("writing", lang) }));
      }
      if (ev.type === "tool.start") {
        const name = String(p.name ?? "tool");
        const args = p.args == null ? "" : JSON.stringify(p.args);
        const id = `live-${seq++}`;
        setStartedAt((v) => v ?? Date.now());
        // text streamed before a tool call is its own reply line
        setStreaming((s) => {
          if (s.trim()) setLive((l) => [...l, { kind: "text", id: `${id}-t`, text: s }]);
          return "";
        });
        const tv = describeTool(id, name, args);
        setLive((l) => [...l, { kind: "tool", id, tool: { ...tv, pending: true } }]);
        // what it is doing, in words rather than the tool's own name; while
        // it waits on its subagents the roster below says the rest
        setActivity((a) => ({ ...a, [sid]: name === "team_delegate" ? t("saAtWork", lang) : [tv.verb, tv.arg].filter(Boolean).join(" ") }));
      }
      if (ev.type === "tool.result") {
        const name = String(p.name ?? "");
        const content = String(p.content ?? "");
        const isError = Boolean(p.isError);
        const kind = String(p.kind ?? "");
        setLive((l) => {
          // results arrive in call order: fill the oldest pending call of that tool
          const idx = l.findIndex((b) => b.kind === "tool" && b.tool.pending && (!name || b.tool.name === name));
          if (idx < 0) return l;
          const b = l[idx];
          if (b.kind !== "tool") return l;
          const next = [...l];
          next[idx] = { ...b, tool: describeTool(b.tool.id, name || b.tool.name, b.tool.args, { content, isError, kind }) };
          return next;
        });
      }
      if (ev.type === "message.done") {
        setStreaming("");
        setLive([]);
        setStartedAt(null);
        setStreamChars(0);
        setActivity((a) => ({ ...a, [sid]: "" }));
        reloadHistorySoon();
      }
    });
    return () => {
      unsub();
      if (streamFor.current === sid) streamFor.current = null;
    };
  }, [activeSession, reloadHistorySoon, activeAgent, lang]);

  useEffect(() => {
    onActivity?.(activity);
  }, [activity, onActivity]);

  // the element the turns scroll inside, either shape of the view
  const scroller = useCallback(
    () => viewRef.current ?? (bottomRef.current?.closest(".term-scroll") as HTMLElement | null),
    [],
  );

  useEffect(() => {
    const vp = scroller();
    if (!vp) return;
    const onScroll = () => {
      // Pulling up stops the view following; coming back to the foot starts
      // it again. Growing text moves the foot away without moving the view,
      // so only a change in position counts.
      if (vp.scrollTop < lastTopRef.current - 2) stickRef.current = false;
      if (vp.scrollHeight - vp.scrollTop - vp.clientHeight < 24) stickRef.current = true;
      lastTopRef.current = vp.scrollTop;
    };
    onScroll();
    vp.addEventListener("scroll", onScroll, { passive: true });
    return () => vp.removeEventListener("scroll", onScroll);
  }, [scroller, activeSession?.id]);

  // Opening a chat lands on its last turn outright. Code blocks, tables and
  // fonts settle a moment after the first paint and push the foot further
  // down, so for a short while every growth follows it; a smooth scroll
  // aimed at the first foot stopped short of the real one.
  useEffect(() => {
    stickRef.current = true;
    const vp = scroller();
    if (!vp || typeof ResizeObserver === "undefined") return;
    const snap = () => { if (stickRef.current) vp.scrollTop = vp.scrollHeight; };
    const ro = new ResizeObserver(snap);
    const watch = () => { const strip = vp.querySelector(".transcript"); if (strip) ro.observe(strip); };
    watch();
    const mo = new MutationObserver(watch);
    mo.observe(vp, { childList: true });
    const done = window.setTimeout(() => { ro.disconnect(); mo.disconnect(); }, 1500);
    return () => { window.clearTimeout(done); ro.disconnect(); mo.disconnect(); };
  }, [scroller, activeSession?.id]);

  useEffect(() => {
    if (!stickRef.current) return;
    const vp = scroller();
    // While the answer streams the foot moves every few milliseconds: jump
    // there outright. A smooth scroll restarted that often never arrives,
    // which is what made the view judder.
    if (vp && streaming) {
      vp.scrollTop = vp.scrollHeight;
      return;
    }
    bottomRef.current?.scrollIntoView({ behavior: variant === "terminal" ? "auto" : "smooth", block: "end" });
    // pendingAsk and busy put the echo and the cloud on the page; the view
    // has to follow those too, or what you just sent stays under the box
  }, [messages, streaming, liveBlocks, variant, scroller, pendingAsk, busy, tail]);

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      const n = e.target as Node;
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
    // a scrollbar only once the text outgrows the box, not on an empty one
    el.style.overflowY = el.scrollHeight > 160 ? "auto" : "hidden";
  }, [input]);

  const switchAgent = useCallback(async (agent: Agent) => {
    setActiveAgent(agent);
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
      space: "chat",
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
      space: "chat",
    });
    await reloadSessions();
    setActiveSession(sess);
    onSession?.(sess);
  }

  const slashHits = !models && !pickSessions && input.startsWith("/") ? filterSlash(input, scope) : [];
  const sessionRows: Session[] = pickSessions ? filterSessions(pickSessions, input) : [];
  const mentionRows: string[] = mention && files && !models && !pickSessions ? filterFiles(files, mention.query) : [];

  // onType follows the text and the caret: a slash command, or an @file
  function onType(e: React.ChangeEvent<HTMLTextAreaElement>) {
    const v = e.target.value;
    setInput(v);
    setSlashHi(0);
    const m = models || pickSessions ? null : mentionAt(v, e.target.selectionStart ?? v.length);
    setMention(m);
    setMentionHi(0);
    if (m) void loadFiles();
  }

  function chooseMention(path: string) {
    if (!mention) return;
    const caret = mention.start + 1 + mention.query.length;
    const next = `${input.slice(0, mention.start)}@${path} ${input.slice(caret)}`;
    setInput(next);
    setMention(null);
    const at = mention.start + path.length + 2;
    requestAnimationFrame(() => { taRef.current?.focus(); taRef.current?.setSelectionRange(at, at); });
  }

  async function addImages(list: File[]) {
    const room = MAX_IMAGES - images.length;
    if (room <= 0) { toast(t("imagesMax", lang).replace("{n}", String(MAX_IMAGES)), "err"); return; }
    const picked = list.filter(isImage).slice(0, room);
    const read = await Promise.all(picked.map(async (f) => ({ name: f.name || "image", url: await imageToDataUrl(f) })));
    setImages((cur) => [...cur, ...read].slice(0, MAX_IMAGES));
  }

  function onPaste(e: React.ClipboardEvent<HTMLTextAreaElement>) {
    const pics = Array.from(e.clipboardData?.files ?? []).filter(isImage);
    if (pics.length === 0) return;
    e.preventDefault();
    void addImages(pics);
  }

  async function chooseEffort(level: string) {
    setEffortState(level);
    if (!activeSession) { pendingEffort.current = level; return; }
    try {
      await rpc("session.setEffort", { sessionId: activeSession.id, effort: level });
      setChatModel((m) => (m ? { ...m, effort: level } : m));
    } catch (e) {
      setSendErr(e instanceof Error ? e.message : String(e));
    }
  }
  // the picker's rows: "back to default" first when the chat has its own
  const RESET: ModelChoice = { provider: "", model: "" };
  // Opened from the model button, the menu has its own search box; typed
  // as /models, the message box is the search.
  const modelSearch = modelsFromPill ? modelQuery : input;
  const modelRows: ModelChoice[] = models
    ? [...(chatModel?.source === "chat" ? [RESET] : []), ...filterModels(models, modelSearch)]
    : [];

  // what this chat runs on (its own pick, its profile's, or its agent's)
  useEffect(() => {
    if (!activeSession) {
      setChatModel(pendingModel.current ? { ...pendingModel.current, source: "chat" } : null);
      return;
    }
    // asking before the agent is known gets an answer for the wrong one,
    // and the real question follows a moment later anyway
    if (!activeAgent?.id) return;
    let live = true;
    rpc<ChatModel>("session.model", { sessionId: activeSession.id, agentId: activeAgent?.id ?? "" })
      .then((m) => { if (live) setChatModel(m ?? null); })
      .catch(() => { if (live) setChatModel(null); });
    return () => { live = false; };
  }, [activeSession?.id, activeAgent?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // the models on offer, refreshed when a provider changes
  useEffect(() => {
    let live = true;
    const load = () => {
      rpc<ModelChoice[]>("model.list")
        .then((list) => { if (live) setModelCatalog(list ?? []); })
        .catch(() => { if (live) setModelCatalog([]); });
    };
    load();
    window.addEventListener("rove:providers", load);
    return () => { live = false; window.removeEventListener("rove:providers", load); };
  }, []);

  // a click outside the model picker closes it
  useEffect(() => {
    if (!models) return;
    const onDoc = (e: MouseEvent) => {
      const el = e.target as Node;
      if (menuRef.current?.contains(el) || taRef.current?.contains(el) || pillRef.current?.contains(el)) return;
      closeModels();
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [models]);
  // …outside the role list
  // …and outside the session list
  useEffect(() => {
    if (!pickSessions) return;
    const onDoc = (e: MouseEvent) => {
      const el = e.target as Node;
      if (menuRef.current?.contains(el) || taRef.current?.contains(el)) return;
      closeSessions();
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [pickSessions]);

  // a chat's queue, images and file list do not follow to another chat
  useEffect(() => { setQueue([]); setImages([]); setMention(null); }, [activeSession?.id]);
  useEffect(() => { setFiles(null); }, [workspaceId]);
  useEffect(() => { setPendingAsk(null); }, [activeSession?.id]);
  useEffect(() => { setEffortState(chatModel?.effort ?? pendingEffort.current ?? ""); }, [chatModel]);
  useEffect(() => {
    // Nothing to look up before we know where we are: asking with neither a
    // chat nor a folder spends a round trip to be told nothing.
    if (!activeSession && !workspacePath) { setRules([]); return; }
    let live = true;
    rpc<{ files: string[] }>("workspace.rules", activeSession ? { sessionId: activeSession.id, workspace: workspacePath ?? "" } : { workspace: workspacePath ?? "" })
      .then((r) => { if (live) setRules(r?.files ?? []); })
      .catch(() => { if (live) setRules([]); });
    return () => { live = false; };
  }, [activeSession?.id, workspacePath]); // eslint-disable-line react-hooks/exhaustive-deps

  const loadFiles = useCallback(async (): Promise<string[]> => {
    if (files) return files;
    if (!workspaceId) return [];
    const list = (await rpc<string[]>("fs.tree", { workspaceId }).catch(() => [])) ?? [];
    const onlyFiles = list.filter((f) => !f.endsWith("/"));
    setFiles(onlyFiles);
    return onlyFiles;
  }, [files, workspaceId]);

  // the highlighted row stays in view as the arrows move it
  useEffect(() => {
    const row = menuRef.current?.querySelector(".slash-row.active") as HTMLElement | null;
    row?.scrollIntoView?.({ block: "nearest" });
  }, [slashHi, mentionHi, models, pickSessions, input]);

  type PaneRow = { n: number; id: string; label: string; role?: string; characterId?: string; running: boolean; task?: string };
  async function paneList(): Promise<PaneRow[]> {
    const sid = activeSession?.id ?? pane?.groupId;
    if (!sid) return [];
    return (await rpc<PaneRow[]>("terminal.panes", { sessionId: sid }).catch(() => [])) ?? [];
  }

  // sendToPane gives another terminal of this session a message to work on
  async function sendToPane(ref: string, message: string) {
    const s = needChat();
    if (!s) return;
    try {
      const to = await rpc<PaneRow>("terminal.sendPane", { from: s.id, to: ref, message });
      toast(`${to.label} ← ${t("paneSent", lang)}`, "ok");
    } catch (e) {
      setSendErr(e instanceof Error ? e.message : String(e));
    }
  }

  // setRole gives this terminal (or another: forId) a character's role
  // openSessions lists this space's chats to switch to (/session)
  // this space's chats, newest first (member channels are not chats)
  async function sessionsHere(): Promise<Session[]> {
    const all = (await rpc<Session[]>("session.list", { workspaceId: "" }).catch(() => [])) ?? [];
    const space = activeSession ? spaceOf(activeSession, badgeFor(activeSession.id)) : "chat";
    return all
      .filter((s) => !s.parentId && spaceOf(s, badgeFor(s.id)) === space)
      .sort((a, b) => (b.updatedAt || "").localeCompare(a.updatedAt || ""));
  }

  async function openSessions(filter = "") {
    const list = await sessionsHere();
    if (list.length === 0) { setSendErr(t("slashNoSessions", lang)); return; }
    setModels(null);
    setPickSessions(list);
    setInput(filter);
    const cur = list.findIndex((s) => s.id === activeSession?.id);
    setSlashHi(filter ? 0 : Math.max(0, cur));
    taRef.current?.focus();
  }

  function closeSessions() {
    setPickSessions(null);
    setInput("");
    setSlashHi(0);
  }

  function chooseSession(s: Session) {
    closeSessions();
    setSendErr("");
    if (s.id === activeSession?.id) return;
    setActiveSession(s);
    onSession?.(s);
  }

  // closeModels shuts the picker and gives back what was being typed
  function closeModels() {
    setModels(null);
    setModelQuery("");
    setInput(stashed.current ?? "");
    stashed.current = null;
    setModelsFromPill(false);
    setSlashHi(0);
  }

  // openModels shows the model picker (from /models, or the model button)
  async function openModels(fromPill: boolean, filter = "") {
    const list = (await rpc<ModelChoice[]>("model.list").catch(() => [])) ?? [];
    setModelCatalog(list);
    if (list.length === 0) { setSendErr(t("slashNoModels", lang)); return; }
    if (fromPill) {
      stashed.current = input;
      setInput("");
    } else {
      setInput(filter);
    }
    setModelsFromPill(fromPill);
    setModels(list);
    const cur = list.findIndex((m) => chatModel && m.model === chatModel.model && (!chatModel.provider || m.provider === chatModel.provider));
    setSlashHi(filter ? 0 : Math.max(0, cur + (chatModel?.source === "chat" ? 1 : 0)));
    taRef.current?.focus();
  }

  async function chooseModel(m: ModelChoice) {
    closeModels();
    setSendErr("");
    const label = m.model ? m.model : t("slashModelDefault", lang);
    if (!activeSession) {
      pendingModel.current = m.model ? m : null;
      setChatModel(m.model ? { ...m, source: "chat" } : null);
      toast(`${t("slashModelSet", lang)}: ${label}`, "ok");
      return;
    }
    try {
      await rpc("session.setModel", { sessionId: activeSession.id, provider: m.provider, model: m.model });
      const now = await rpc<ChatModel>("session.model", { sessionId: activeSession.id, agentId: activeAgent?.id ?? "" }).catch(() => null);
      setChatModel(now ?? (m.model ? { ...m, source: "chat" } : null));
      toast(`${t("slashModelSet", lang)}: ${label}`, "ok");
    } catch (e) {
      setSendErr(e instanceof Error ? e.message : String(e));
    }
  }

  // a command's argument placeholder in the app's language
  function argFor(c: SlashCmd | undefined): string {
    if (!c?.arg) return "";
    const key = `slashArg_${c.id}`;
    const v = t(key, lang);
    return v === key ? c.arg : v;
  }

  function hintFor(id: SlashId): string {
    const keys: Record<SlashId, string> = {
      new: "slashNew", undo: "slashUndo", redo: "slashRedo", stop: "slashStop",
      clear: "slashClear", help: "slashHelp",
      map: "slashMap", context: "slashContext",
      goal: "slashGoal", rename: "slashRename", export: "slashExport",
      remember: "slashRemember", checkpoint: "slashCheckpoint", restore: "slashRestore", usage: "slashUsage",
      models: "slashModels", session: "slashSession", diff: "slashDiff", compact: "slashCompact",
      panes: "slashPanes", to: "slashTo", all: "slashAll",
    };
    return t(keys[id], lang);
  }

  async function applyKeep(keep: number) {
    if (!activeSession) return;
    await rpc("session.truncate", { sessionId: activeSession.id, keep });
    await reloadHistory();
  }

  // commands that act on a chat need one; a draft has none yet
  function needChat(): Session | null {
    if (!activeSession) setSendErr(t("slashNeedChat", lang));
    return activeSession;
  }
  const usage = (id: SlashId) => {
    const c = SLASH.find((x) => x.id === id);
    setSendErr(`/${id} ${argFor(c)} — ${hintFor(id)}`);
  };

  async function runSlash(id: SlashId, rest: string) {
    setInput("");
    setSendErr("");
    switch (id) {
      case "goal": {
        // "/goal" alone resumes this chat's stopped goal
        if (!rest.trim() && activeSession) {
          type G = { id: string; title: string; status: string; sessionId?: string };
          const list = (await rpc<G[]>("goal.list").catch(() => [])) ?? [];
          const paused = list.filter((x) => x.sessionId === activeSession.id && (x.status === "pending" || x.status === "canceled")).pop();
          if (paused) {
            await rpc("goal.drive", { id: paused.id, sessionId: activeSession.id, workspace: workspacePath ?? "" });
            toast(`${t("slashGoalResumed", lang)}: ${paused.title}`, "ok");
            return;
          }
        }
        const g = parseGoal(rest);
        if (!g) { usage("goal"); return; }
        // a goal runs in a chat: a draft becomes one first
        let target = activeSession;
        if (!target && draft && onCreateSession) {
          target = await onCreateSession();
          activeIdRef.current = target.id;
          setActiveSession(target);
        }
        if (!target) return;
        // the goal works in this chat, with the agent it talks to
        const goal = await rpc<{ id: string }>("goal.create", {
          title: g.title,
          workspaceId: workspaceId ?? "",
          sessionId: target.id,
          agentId: activeAgent?.id ?? "",
          completionContract: { criteria: g.criteria, maxIterations: 8 },
        });
        await rpc("goal.drive", { id: goal.id, sessionId: target.id, workspace: workspacePath ?? "" });
        toast(`${t("slashGoalStarted", lang)}: ${g.title}`, "ok");
        return;
      }
      case "rename": {
        const s = needChat();
        if (!s) return;
        if (!rest) { usage("rename"); return; }
        await rpc("session.rename", { id: s.id, title: rest.slice(0, 80) });
        window.dispatchEvent(new Event("rove:sessions"));
        return;
      }
      case "export":
        if (needChat()) await exportSession();
        return;
      case "remember": {
        if (!rest) { usage("remember"); return; }
        // project notes ride along on every turn, so keep them short
        await rpc("memory.put", {
          scope: workspaceId ? "workspace" : "global",
          scopeId: workspaceId ?? "",
          key: rest.split(/\s+/).slice(0, 4).join(" ").slice(0, 40),
          content: rest.slice(0, 280),
        });
        toast(t("slashRemembered", lang), "ok");
        return;
      }
      case "checkpoint": {
        if (!workspacePath) { setSendErr(t("slashNeedWorkspace", lang)); return; }
        await rpc("checkpoint.take", { path: workspacePath, label: rest || "manual" });
        toast(t("slashCheckpointTaken", lang), "ok");
        return;
      }
      case "restore": {
        if (!workspacePath) { setSendErr(t("slashNeedWorkspace", lang)); return; }
        const snaps = await rpc<{ ref: string }[]>("checkpoint.list", { path: workspacePath });
        if (!Array.isArray(snaps) || snaps.length === 0) { setSendErr(t("slashNoCheckpoint", lang)); return; }
        await rpc("checkpoint.restore", { path: workspacePath, ref: snaps[0].ref });
        toast(t("slashRestored", lang), "ok");
        return;
      }
      case "usage": {
        const u = await rpc<{ totalTokens: number; calls: number; promptTokens?: number; completionTokens?: number }>("usage.get");
        toast(`${compact(u.totalTokens)} token · ${u.calls} ${t("calls", lang)} (in ${compact(u.promptTokens ?? 0)} · out ${compact(u.completionTokens ?? 0)})`, "ok");
        return;
      }
      case "context":
        onOpenContext?.();
        return;
      case "map": {
        if (!rest) { onOpenMap?.(); return; }
        if (!activeSession) return;
        // Share only the few best files: this lands in the transcript and is
        // sent on the next turn, so it stays small on purpose.
        const res = await rpc<{ hits: MapHit[] }>("codemap.query", { sessionId: activeSession.id, q: rest, limit: 5 });
        const files = (res.hits ?? []).map((h) => h.file);
        if (files.length === 0) {
          toast(`/map: ${rest} —`, "err");
          return;
        }
        await rpc("codemap.share", { sessionId: activeSession.id, files, note: `/map ${rest}` });
        await reloadHistory();
        toast(t("mapShared", lang), "ok");
        return;
      }
      case "new":
        undoStack.current = [];
        if (onNewDraft) onNewDraft();
        else await newSession();
        return;
      case "stop": {
        // /stop 2 (or /stop all) stops other terminals of the session
        if (pane && rest) {
          const list = await paneList();
          const targets = /^(all|hepsi)$/i.test(rest.trim())
            ? list.filter((p) => p.id !== activeSession?.id)
            : list.filter((p) => p.label.toLowerCase() === (/^\d+$/.test(rest.trim()) ? `t${rest.trim()}` : rest.trim().toLowerCase()));
          if (targets.length === 0) { setSendErr(`${t("paneNone", lang)}: ${rest}`); return; }
          await Promise.all(targets.map((p) => rpc("session.cancel", { sessionId: p.id }).catch(() => {})));
          toast(`${targets.map((p) => p.label).join(", ")} ${t("paneStopped", lang)}`, "ok");
          return;
        }
        await stop();
        return;
      }
      case "panes": {
        const list = await paneList();
        setNote(list.map((p) => `${p.label}${p.id === activeSession?.id ? " ◂" : ""} · ${p.running ? t("paneWorking", lang) : t("paneIdle", lang)}${p.task ? ` — ${p.task}` : ""}`).join("\n") || t("paneOnlyOne", lang));
        return;
      }
      case "to": {
        const m = /^(\S+)\s+([\s\S]+)$/.exec(rest.trim());
        if (!m) { usage("to"); return; }
        await sendToPane(m[1], m[2]);
        return;
      }
      case "all": {
        if (!rest.trim()) { usage("all"); return; }
        const s = needChat();
        if (!s) return;
        const others = (await paneList()).filter((p) => p.id !== s.id);
        if (others.length === 0) { setSendErr(t("paneOnlyOne", lang)); return; }
        const sent: string[] = [];
        const busy: string[] = [];
        for (const p of others) {
          try { await rpc("terminal.sendPane", { from: s.id, to: p.label, message: rest }); sent.push(p.label); } catch { busy.push(p.label); }
        }
        toast(`${sent.length ? `${sent.join(", ")} ← ${t("paneSent", lang)}` : ""}${busy.length ? ` · ${busy.join(", ")} ${t("paneBusy", lang)}` : ""}`, busy.length && !sent.length ? "err" : "ok");
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
        if (workspacePath) {
          try {
            type Snap = { ref: string };
            const snaps = await rpc<Snap[]>("checkpoint.list", { path: workspacePath });
            if (Array.isArray(snaps) && snaps.length > 0) {
              await rpc("checkpoint.restore", { path: workspacePath, ref: snaps[0].ref });
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
      case "models": {
        if (rest) {
          const list = (await rpc<ModelChoice[]>("model.list").catch(() => [])) ?? [];
          const hit = pickModel(list, rest);
          if (hit) { await chooseModel(hit); return; }
        }
        // open the picker, filtered by what was typed after the command
        await openModels(false, rest);
        return;
      }
      case "diff": {
        const s = needChat();
        if (!s) return;
        await reloadEdits();
        setReviewOpen(true);
        return;
      }
      case "compact": {
        const s = needChat();
        if (!s) return;
        const r = await rpc<{ compacted: boolean }>("session.compact", { sessionId: s.id, agentId: activeAgent?.id ?? "" });
        toast(r.compacted ? t("compactDone", lang) : t("compactNothing", lang), "ok");
        await reloadHistory();
        return;
      }
      case "session": {
        if (rest) {
          const hits = filterSessions(await sessionsHere(), rest);
          if (hits.length === 1) { chooseSession(hits[0]); return; }
        }
        await openSessions(rest);
        return;
      }
      case "help":
        setSendErr(SLASH.map((c) => `/${c.id} — ${hintFor(c.id)}`).join(" · "));
        return;
      default:
        return;
    }
  }

  async function send() {
    // with a picker open, sending picks its highlighted row
    if (pickSessions) {
      const pick = sessionRows[Math.min(slashHi, sessionRows.length - 1)];
      if (pick) chooseSession(pick);
      return;
    }
    if (models) {
      const pick = modelRows[Math.min(slashHi, modelRows.length - 1)];
      if (pick) await chooseModel(pick);
      return;
    }
    const msg = input.trim();
    // @T2 message: work for another terminal of this session
    const toPane = pane ? paneRef(msg) : null;
    if (toPane) {
      await sendToPane(toPane.ref, toPane.message);
      setInput("");
      return;
    }
    const hit = matchSlash(msg, scope);
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
    if ((!msg && images.length === 0) || !activeAgent) return;
    if (!activeSession && !(draft && onCreateSession)) return;
    // written while the agent is still answering: it waits its turn
    if (sending) {
      setQueue((q) => [...q, { id: `${Date.now()}-${q.length}`, text: msg, images }]);
      setInput("");
      setImages([]);
      setMention(null);
      return;
    }
    await deliver(msg, images);
  }

  // deliver sends one message (with its images and @files) to the chat,
  // creating the chat first when it is a draft.
  async function deliver(msg: string, imgs: ImageAttachment[]) {
    stickRef.current = true;
    if (!activeAgent) return;
    const text = msg || t("imageOnly", lang);
    // @codebase mention: search the codebase index and prepend results as context.
    let content = text;
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
          content = `<codebase-context>\n${ctx}\n</codebase-context>\n\n${text}`;
        }
      } catch {
        // index unavailable — send without context
      }
    }

    // @path mentions: the files' content goes with the message
    if (text.includes("@")) {
      const known = new Set(await loadFiles());
      const paths = mentionedPaths(text, known);
      if (paths.length > 0) {
        const read = await Promise.all(paths.map(async (path) => {
          const r = await rpc<{ content: string }>("fs.read", { path, workspaceId: workspaceId ?? "" }).catch(() => null);
          return r ? { path, content: r.content } : null;
        }));
        content = withFiles(content, read.filter((x): x is { path: string; content: string } => x !== null));
      }
    }

    setInput("");
    setImages([]);
    setMention(null);
    setSendErr("");
    setStartedAt(Date.now());
    setPendingAsk({ text: content, images: imgs.map((i) => i.url) });
    let target = activeSession;
    try {
      if (!target && onCreateSession) {
        setCreating(true);
        try {
          target = await onCreateSession();
        } finally {
          setCreating(false);
        }
        streamFor.current = target.id;
        activeIdRef.current = target.id;
        setActiveSession(target);
        // a model picked while this was a draft
        if (pendingModel.current) {
          const m = pendingModel.current;
          pendingModel.current = null;
          await rpc("session.setModel", { sessionId: target.id, provider: m.provider, model: m.model }).catch(() => {});
        }
        if (pendingEffort.current) {
          const level = pendingEffort.current;
          pendingEffort.current = null;
          await rpc("session.setEffort", { sessionId: target.id, effort: level }).catch(() => {});
        }
      }
      if (!target) return;
      markBusy(target.id, true);
      setActivity((a) => ({ ...a, [target!.id]: t("thinking", lang) }));
      await rpc("session.send", {
        sessionId: target.id,
        agentId: activeAgent.id,
        workspaceId: workspaceId ?? "",
        content,
        images: imgs.map((i) => i.url),
      });
      if (activeIdRef.current === target.id) reloadHistorySoon();
    } catch (err) {
      const fail = err instanceof Error ? err.message : "send failed";
      setPendingAsk(null);
      toast(fail, "err");
      // give the text back only if the user is still looking at that chat
      if (activeIdRef.current === (target?.id ?? null)) {
        setInput(msg);
        setImages(imgs);
        setSendErr(fail);
      }
    } finally {
      if (target) {
        const id = target.id;
        markBusy(id, false);
        setActivity((a) => ({ ...a, [id]: "" }));
      }
    }
  }

  // the next queued message goes as soon as the agent is free
  useEffect(() => {
    if (sending || queue.length === 0 || !activeSession) return;
    const [next, ...rest] = queue;
    setQueue(rest);
    void deliver(next.text, next.images);
  }, [sending, queue, activeSession]); // eslint-disable-line react-hooks/exhaustive-deps

  // steer: this queued message goes now — the running reply is stopped
  function sendNow(item: Queued) {
    setQueue((q) => [item, ...q.filter((x) => x.id !== item.id)]);
    void stop();
  }

  async function stop() {
    if (!activeAgent) return;
    // stop this session only; other sessions may share the agent
    if (!activeSession) return;
    const id = activeSession.id;
    await rpc("session.cancel", { sessionId: id }).catch(() => {});
    markBusy(id, false);
    setStreaming("");
    setActivity((a) => ({ ...a, [id]: "" }));
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (mention && mentionRows.length > 0) {
      const n = mentionRows.length;
      if (e.key === "ArrowDown") { e.preventDefault(); setMentionHi((i) => (i + 1) % n); return; }
      if (e.key === "ArrowUp") { e.preventDefault(); setMentionHi((i) => (i - 1 + n) % n); return; }
      if ((e.key === "Enter" && !e.shiftKey) || e.key === "Tab") { e.preventDefault(); chooseMention(mentionRows[Math.min(mentionHi, n - 1)]); return; }
      if (e.key === "Escape") { e.preventDefault(); setMention(null); return; }
    }
    if (pickSessions) {
      const n = sessionRows.length;
      if (e.key === "ArrowDown" && n) { e.preventDefault(); setSlashHi((i) => (i + 1) % n); return; }
      if (e.key === "ArrowUp" && n) { e.preventDefault(); setSlashHi((i) => (i - 1 + n) % n); return; }
      if ((e.key === "Enter" && !e.shiftKey) || e.key === "Tab") {
        e.preventDefault();
        const pick = sessionRows[Math.min(slashHi, n - 1)];
        if (pick) chooseSession(pick);
        return;
      }
      if (e.key === "Escape") { e.preventDefault(); closeSessions(); return; }
    }
    if (models) {
      const n = modelRows.length;
      if (e.key === "ArrowDown" && n) { e.preventDefault(); setSlashHi((i) => (i + 1) % n); return; }
      if (e.key === "ArrowUp" && n) { e.preventDefault(); setSlashHi((i) => (i - 1 + n) % n); return; }
      if ((e.key === "Enter" && !e.shiftKey) || e.key === "Tab") {
        e.preventDefault();
        const pick = modelRows[Math.min(slashHi, n - 1)];
        if (pick) void chooseModel(pick);
        return;
      }
      if (e.key === "Escape") { e.preventDefault(); closeModels(); return; }
    }
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

  async function attachFiles(picked: FileList | File[]) {
    const all = Array.from(picked);
    const pics = all.filter(isImage);
    if (pics.length) void addImages(pics);
    const list = all.filter((f) => !isImage(f));
    if (!list.length) return;
    const bits = await Promise.all(list.map(async (f) => {
      const text = await f.text().catch(() => "");
      const body = text.slice(0, 12_000);
      return `### ${f.name}\n\`\`\`\n${body}\n\`\`\``;
    }));
    setInput((cur) => (cur ? `${cur}\n\n${bits.join("\n\n")}` : bits.join("\n\n")));
  }

  type StreamMsg = { id: string; role: string; content: string; streaming: true };
  // Tool results are shown as chips on the call that made them, not as
  // messages of their own; empty assistant turns that only called tools
  // still show their chips.
  const toolResults = new Map<string, ToolOutcome>();
  for (const m of messages) {
    if (m.role === "tool" && m.toolResult?.toolCallId) toolResults.set(m.toolResult.toolCallId, { content: m.toolResult.content, isError: m.toolResult.isError, kind: m.toolResult.kind });
  }
  const shownMessages = messages.filter((m) => m.kind === "summary" || (m.role !== "tool" && m.role !== "system" && (m.content.trim() || (m.toolCalls?.length ?? 0) > 0)));
  // the echo stands in only until the real message comes back, so it never
  // shows twice and needs no clearing of its own
  const echoed = pendingAsk && !shownMessages.some((m) => m.role === "user" && m.content === pendingAsk.text)
    ? [{ id: "__ask__", sessionId: activeSession?.id ?? "", role: "user" as const, content: pendingAsk.text, images: pendingAsk.images, createdAt: "" }]
    : [];
  const withEcho = [...shownMessages, ...echoed];
  const allMessages: (Message | StreamMsg)[] = streaming
    ? [...withEcho, { id: "__stream__", role: "assistant", content: streaming, streaming: true }]
    : withEcho;

  const live = activeSession ? activity[activeSession.id] ?? "" : "";
  // while the agent answers, Enter queues the message instead
  const canSend = Boolean((input.trim() || images.length > 0) && (activeSession || (draft && onCreateSession)));
  const canType = Boolean(activeSession || (draft && onCreateSession));
  const empty = allMessages.length === 0;

  // Follow the scroll: the pinned line is the last prompt that has gone off
  // the top. At the very top of a chat there is nothing above, so it hides.
  useEffect(() => {
    const vp = viewRef.current;
    if (!vp) return;
    const read = () => {
      // a prompt pins as soon as its first line leaves, so a long one is
      // still named while you are reading through it
      const edge = vp.getBoundingClientRect().top + 1;
      let text = "";
      for (const el of Array.from(vp.querySelectorAll<HTMLElement>("[data-prompt]"))) {
        if (el.getBoundingClientRect().top > edge) break;
        text = el.dataset.prompt ?? "";
      }
      setPinned(text);
    };
    read();
    vp.addEventListener("scroll", read, { passive: true });
    return () => {
      vp.removeEventListener("scroll", read);
    };
  }, [allMessages.length, activeSession?.id]);

  // The pinned line and the message box sit on the thread, the plates inside
  // the scroller — so a scrollbar, or the sidebar folding away, moves one and
  // not the others. Measuring the plates and handing that box to both keeps
  // the three edges flush whatever the width does. A scroll cannot change it,
  // a resize of either box always can, so that is what we watch.
  useEffect(() => {
    const vp = viewRef.current;
    const frame = threadRef.current;
    if (!vp || !frame) return;
    const measure = () => {
      const strip = vp.querySelector<HTMLElement>(".transcript");
      if (!strip) return;
      const a = strip.getBoundingClientRect();
      const f = frame.getBoundingClientRect();
      const box = { left: Math.round(a.left - f.left), width: Math.round(a.width) };
      setPinBox((old) => (old && old.left === box.left && old.width === box.width ? old : box));
    };
    measure();
    window.addEventListener("resize", measure);
    // jsdom has no ResizeObserver; the resize listener carries the tests
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    ro?.observe(vp);
    ro?.observe(frame);
    return () => {
      window.removeEventListener("resize", measure);
      ro?.disconnect();
    };
  }, [activeSession?.id, empty]);

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

  // What the model button shows: the chat's model (its own pick, its
  // profile's or its agent's), but only when a provider still offers it.
  // Otherwise there is nothing to name, and the button says so quietly.
  const offered = (m?: { provider?: string; model?: string } | null) =>
    Boolean(m?.model && modelCatalog?.some((c) => c.model === m.model && (!m.provider || !c.provider || c.provider === m.provider)));
  const shown = offered(chatModel) ? chatModel : offered(activeAgent) ? activeAgent : null;
  const modelLabel = shown?.model ?? NO_MODEL;
  const modelProvider = shown?.provider ?? "";
  const isCurrent = (m: ModelChoice) => Boolean(chatModel && m.model === chatModel.model && (!chatModel.provider || !m.provider || m.provider === chatModel.provider));
  const modelMenuEl = models ? (
    <div className={`slash-menu model-menu${modelsFromPill ? " from-pill" : ""}`} role="listbox" aria-label={t("slashModels", lang)} ref={menuRef}>
      <div className="slash-head">
        <span>{t("slashModels", lang)}</span>
        {chatModel?.model && <span className="term-dim">{t("slashModelNow", lang)}: {chatModel.model}</span>}
      </div>
      {modelsFromPill && (
        <div className="model-search">
          <input
            autoFocus
            value={modelQuery}
            placeholder={t("modelSearch", lang)}
            aria-label={t("modelSearch", lang)}
            onChange={(e) => { setModelQuery(e.target.value); setSlashHi(0); }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") { e.preventDefault(); setSlashHi((h) => Math.min(h + 1, modelRows.length - 1)); }
              else if (e.key === "ArrowUp") { e.preventDefault(); setSlashHi((h) => Math.max(h - 1, 0)); }
              else if (e.key === "Enter") { e.preventDefault(); const m = modelRows[slashHi]; if (m) { void chooseModel(m); taRef.current?.focus(); } }
              else if (e.key === "Escape") { e.preventDefault(); closeModels(); taRef.current?.focus(); }
            }}
          />
        </div>
      )}
      {/* how hard a reasoning model thinks; models without it ignore it */}
      <div className="effort-row" role="radiogroup" aria-label={t("effortLabel", lang)}>
        <span>{t("effortLabel", lang)}</span>
        {EFFORTS.map((level) => (
          <button
            type="button"
            role="radio"
            key={level || "auto"}
            aria-checked={effort === level}
            className={effort === level ? "on" : ""}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => void chooseEffort(level)}
          >
            {t(`effort_${level || "auto"}`, lang)}
          </button>
        ))}
      </div>
      {modelRows.length === 0 && <div className="slash-empty">{t("noneYet", lang)}</div>}
      {modelRows.map((m, i) => (
        <Fragment key={`${m.provider}/${m.model}`}>
          {/* models are grouped under their provider */}
          {m.model && m.provider !== modelRows[i - 1]?.provider && (
            <div className="model-group">
              {m.provider}
              {m.default && <span> · {t("slashModelDefaultProvider", lang)}</span>}
            </div>
          )}
          <button
            type="button"
            role="option"
            aria-selected={i === slashHi}
            className={`slash-row${i === slashHi ? " active" : ""}${isCurrent(m) ? " current" : ""}`}
            onMouseMove={() => i !== slashHi && setSlashHi(i)}
            onClick={() => { void chooseModel(m); taRef.current?.focus(); }}
          >
            {m.model ? (
              <>
                <code>{m.model}</code>
                {isCurrent(m) && <span className="slash-check">✓</span>}
              </>
            ) : (
              <code className="slash-reset">↺ {t("slashModelDefault", lang)}</code>
            )}
          </button>
        </Fragment>
      ))}
    </div>
  ) : null;

  const sessionMenuEl = pickSessions ? (
    <div className="slash-menu session-menu" role="listbox" aria-label={t("slashSessionPick", lang)} ref={menuRef}>
      <div className="slash-head">
        <span>{t("slashSessionPick", lang)}</span>
        <span className="term-dim">↑↓ · Enter</span>
      </div>
      {sessionRows.length === 0 && <div className="slash-empty">{t("noneYet", lang)}</div>}
      {sessionRows.map((s, i) => (
        <button
          type="button"
          role="option"
          aria-selected={i === slashHi}
          key={s.id}
          className={`slash-row${i === slashHi ? " active" : ""}${s.id === activeSession?.id ? " current" : ""}`}
          onMouseMove={() => i !== slashHi && setSlashHi(i)}
          onClick={() => { chooseSession(s); taRef.current?.focus(); }}
        >
          <strong className="session-title">{s.title || t("newSession", lang)}</strong>
          <span>{ago(s.updatedAt, lang)}</span>
          {s.id === activeSession?.id && <span className="slash-check">✓</span>}
        </button>
      ))}
    </div>
  ) : null;

  const mentionMenuEl = mentionRows.length > 0 ? (
    <div className="slash-menu mention-menu" role="listbox" aria-label={t("mentionFiles", lang)} ref={menuRef}>
      <div className="slash-head"><span>{t("mentionFiles", lang)}</span><span className="term-dim">Tab · Enter</span></div>
      {mentionRows.map((f, i) => (
        <button
          type="button"
          role="option"
          aria-selected={i === mentionHi}
          key={f}
          className={`slash-row${i === mentionHi ? " active" : ""}`}
          onMouseMove={() => i !== mentionHi && setMentionHi(i)}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => chooseMention(f)}
        >
          <code>{f.slice(f.lastIndexOf("/") + 1)}</code>
          <span>{f}</span>
        </button>
      ))}
    </div>
  ) : null;

  const slashMenuEl = modelMenuEl ?? sessionMenuEl ?? mentionMenuEl ?? (slashHits.length > 0 ? (
    <div className="slash-menu" role="listbox" ref={menuRef}>
      {slashHits.map((c: SlashCmd, i) => (
        <button
          type="button"
          key={c.id}
          className={`slash-row${i === slashHi ? " active" : ""}`}
          onMouseMove={() => i !== slashHi && setSlashHi(i)}
          onClick={() => { setInput(`/${c.id} `); taRef.current?.focus(); }}
        >
          <code>/{c.id}{c.arg ? <em> {argFor(c)}</em> : null}</code>
          <span>{hintFor(c.id)}</span>
        </button>
      ))}
    </div>
  ) : null);


  const queueEl = queue.length > 0 ? (
    <div className="status-stack queue-list" aria-label={t("queueTitle", lang)}>
      {queue.map((q) => (
        <div key={q.id} className="queue-item">
          <span className="queue-label">{t("queued", lang)}</span>
          <span className="queue-text" title={q.text}>{q.text || `🖼 ×${q.images.length}`}</span>
          {q.images.length > 0 && q.text && <span className="term-dim">🖼 {q.images.length}</span>}
          <button type="button" className="ghost" title={t("queueNowHint", lang)} onClick={() => sendNow(q)}>{t("queueNow", lang)}</button>
          <button type="button" className="tree-icon" title={t("delete", lang)} onClick={() => setQueue((list) => list.filter((x) => x.id !== q.id))}>×</button>
        </div>
      ))}
    </div>
  ) : null;

  const imagesEl = images.length > 0 ? (
    <div className="composer-images">
      {images.map((im, i) => (
        <span key={i} className="composer-image" title={im.name}>
          <img src={im.url} alt={im.name} />
          <button type="button" aria-label={t("delete", lang)} onClick={() => setImages((list) => list.filter((_, j) => j !== i))}>×</button>
        </span>
      ))}
    </div>
  ) : null;

  const rulesEl = rules.length > 0 ? (
    <span className="persona-chip static rules-chip" title={`${t("rulesHint", lang)}: ${rules.join(", ")}`}>
      <Icon name="check" size={11} /> {rules[0]}{rules.length > 1 ? ` +${rules.length - 1}` : ""}
    </span>
  ) : null;

  // Which folder the agent is about to touch. It is the one thing you cannot
  // afford to guess about, and the chat otherwise never says it — the folder
  // only shows in the session list, beside chats you are not looking at.
  const folderEl = workspacePath ? (
    <span className="persona-chip static folder-chip" title={workspacePath}>
      <Icon name="folder" size={11} /> {workspacePath.split("/").filter(Boolean).pop() || workspacePath}
    </span>
  ) : null;

  const reviewEl = reviewOpen && activeSession ? (
    <ReviewSheet sessionId={activeSession.id} view={edits} setView={setEdits} onClose={() => setReviewOpen(false)} lang={lang} />
  ) : null;

  // How much blank to keep under the last question. A message sent into a
  // short chat lands on the bottom edge with the message box over it and no
  // way to scroll it anywhere; keeping room below lets it rise to a little
  // above the middle, where you can watch the answer arrive. RISE is where
  // the question's top ends up, as a share of the height. The room needed is
  // whatever the turn itself does not already fill, so it closes by itself as
  // the answer grows and is nothing once the answer is long.
  const RISE = 0.42;
  useEffect(() => {
    const vp = viewRef.current;
    if (!vp) return;
    const measure = () => {
      const strip = vp.querySelector<HTMLElement>(".transcript");
      const asks = strip?.querySelectorAll<HTMLElement>(".turn-user");
      const last = asks?.length ? asks[asks.length - 1] : null;
      if (!strip || !last) {
        setTail((old) => (old === 0 ? old : 0));
        return;
      }
      const tailEl = strip.querySelector<HTMLElement>(".thread-tail");
      const below = strip.getBoundingClientRect().bottom
        - last.getBoundingClientRect().top
        - (tailEl?.getBoundingClientRect().height ?? 0);
      const want = Math.max(0, Math.round(vp.clientHeight * (1 - RISE) - below));
      setTail((old) => (Math.abs(old - want) < 2 ? old : want));
    };
    measure();
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    ro?.observe(vp);
    window.addEventListener("resize", measure);
    return () => {
      ro?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [allMessages.length, streaming, liveBlocks, activeSession?.id, busy]);

  if (variant === "terminal") {
    const termKey = (e: React.KeyboardEvent) => {
      if (e.key === "Escape" && busy && slashHits.length === 0 && !models && !pickSessions) {
        e.preventDefault();
        void stop();
        return;
      }
      handleKeyDown(e);
    };
    return (
      <div className="term" onMouseDown={onFocusPane}>
        <div className="term-scroll">
          <TermTranscript
            banner={{
              title: `Rove Code v${APP_VERSION}`,
              // the model that will answer, or that none is picked — never
              // a default name no connected account offers
              line1: shown ? `${shown.provider ? `${shown.provider} · ` : ""}${shown.model}` : t("termNoModel", lang),
              line2: (workspacePath || activeSession?.title || "").replace(/^\/(Users|home)\/[^/]+/, "~"),
            }}
            blocks={toBlocks(messages)}
            live={liveBlocks}
            streaming={streaming}
            busy={busy}
            startedAt={startedAt}
            tokens={estimateTokens(streamChars)}
          />
          <div ref={bottomRef} />
        </div>
        {sendErr && <div className="term-err">{sendErr}</div>}
        {note && (
          <pre className="term-note" role="note">
            {note}
            <button type="button" className="tree-icon" aria-label={t("close", lang)} onClick={() => setNote("")}>×</button>
          </pre>
        )}
        {queueEl}
        {dock}
        {activeSession && (
          <ChangesBar sessionId={activeSession.id} view={edits} setView={setEdits} onOpen={() => setReviewOpen(true)} lang={lang} compact />
        )}
        {imagesEl}
        <form className="term-input" onSubmit={(e) => { e.preventDefault(); void send(); }}>
          {slashMenuEl}
          <span className="term-prompt">›</span>
          <textarea
            ref={taRef}
            className="term-textarea"
            rows={1}
            value={input}
            onChange={onType}
            onPaste={onPaste}
            onKeyDown={termKey}
            placeholder={t("termPlaceholder", lang)}
            disabled={!canType}
            spellCheck={false}
          />
        </form>
        <div className="term-mode">
          <span className="term-dim term-mode-hint">{t("termHint", lang)}</span>
          {rules.length > 0 && <span className="term-dim term-rules" title={rules.join(", ")}>{rules[0]}</span>}
          <button
            type="button"
            ref={pillRef}
            className="term-model"
            aria-label={t("modelPick", lang)}
            onClick={() => { if (models) closeModels(); else void openModels(true); }}
          >
            {shown ? modelLabel : t("termPickModel", lang)} <span className="term-dim">⌄</span>
          </button>
        </div>
      {reviewEl}
      </div>
    );
  }

  return (
    <div
      className="thread"
      ref={threadRef}
      onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        if (e.dataTransfer.files.length) void attachFiles(e.dataTransfer.files);
      }}
    >
      {pinned && (
        <div className="thread-pin" title={pinned} style={pinBox ? { left: pinBox.left, width: pinBox.width } : undefined}>
          {pinned}
        </div>
      )}
      <div className="thread-viewport" ref={viewRef}>
        {empty ? (
          <div className="intro">
            <img className="wordmark-logo" src={wordmarkLogo} alt="" />
            <p className="wordmark" aria-label="ROVE"><span>ROVE</span></p>
            <p className="intro-body">{t("introBody", lang)}</p>
          </div>
        ) : (
          <div className="transcript">
            {allMessages.map((m) => {
              const isUser = m.role === "user";
              const streamingMsg = "streaming" in m;
              if (!streamingMsg && "kind" in m && m.kind === "summary") {
                // older messages were summarized for the model; they stay above
                return (
                  <div key={m.id} className="turn turn-summary">
                    <details>
                      <summary>{t("compactedDivider", lang)}</summary>
                      <Markdown text={m.content} />
                    </details>
                  </div>
                );
              }
              const shown = isUser ? splitContext(m.content) : null;
              const pics = !streamingMsg && "images" in m ? m.images ?? [] : [];
              return (
                <div key={m.id} className={isUser ? "turn turn-user" : "turn turn-assistant"} data-role={m.role} data-prompt={isUser && shown ? firstLine(shown.text) : undefined}>
                  {isUser && shown ? (
                    <UserPlate>
                      {(shown.files.length > 0 || shown.codebase) && (
                        <div className="msg-files">
                          {shown.files.map((f) => <span key={f} className="msg-file" title={f}>@{f.slice(f.lastIndexOf("/") + 1)}</span>)}
                          {shown.codebase && <span className="msg-file">@codebase</span>}
                        </div>
                      )}
                      {pics.length > 0 && (
                        <div className="msg-images">
                          {pics.map((src, i) => <img key={i} src={src} alt="" />)}
                        </div>
                      )}
                      <Markdown text={shown.text} />
                    </UserPlate>
                  ) : (
                    <div className={`assistant-copy${streamingMsg ? " streaming" : ""}`}>
                      {m.content ? <Markdown text={m.content} /> : (streamingMsg ? "▍" : "")}
                      {!streamingMsg && "toolCalls" in m && m.toolCalls && m.toolCalls.length > 0 && (
                        <ToolRows calls={m.toolCalls} results={toolResults} lang={lang} />
                      )}
                    </div>
                  )}
                  {!streamingMsg && m.content && (
                    <button
                      type="button"
                      className="copy-msg"
                      onClick={() => void navigator.clipboard.writeText(m.content)}
                      aria-label={t("copy", lang)}
                      title={t("copy", lang)}
                    >
                      <Icon name="copy" size={13} />
                    </button>
                  )}
                </div>
              );
            })}
            {busy && !streaming && <Thinking label={live} lang={lang} />}
            <div className="thread-tail" style={{ height: tail }} />
            <div ref={bottomRef} />
          </div>
        )}
      </div>

      <div
        className={`composer-dock${input.trim() || queue.length > 0 ? " filled" : ""}`}
        style={pinBox ? { left: pinBox.left, width: pinBox.width, transform: "none" } : undefined}
      >
        {sendErr && <div className="status-stack" style={{ color: "var(--bad)" }}>{sendErr}</div>}
        {note && <div className="status-stack term-note-chat" role="note"><pre>{note}</pre><button type="button" className="tree-icon" aria-label={t("close", lang)} onClick={() => setNote("")}>×</button></div>}
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
        {queueEl}
        {dock}
        {activeSession && (
          <ChangesBar sessionId={activeSession.id} view={edits} setView={setEdits} onOpen={() => setReviewOpen(true)} lang={lang} />
        )}
        {(team || folderEl || (activeSession && (persona || rules.length > 0))) && (
          <div className="persona-bar">
            {team}
            {/* who this chat talks to; fixed for the chat, picked in the Agents tab */}
            {!team && persona && (
              <span className="persona-chip static">
                {agentBadge}
                <span className="persona-chip-name">{agentName || t("agentGeneral", lang)}</span>
              </span>
            )}
            {rulesEl}
            {folderEl}
          </div>
        )}

        <form className="composer-root" data-slot="composer-root" onSubmit={(e) => { e.preventDefault(); void send(); }}>
          {slashMenuEl}
          {attachOpen && (
            <div className="composer-panel composer-panel-left" ref={attachRef}>
              <div className="section-label" style={{ paddingTop: 8 }}>{t("sessions", lang)}</div>
              <button type="button" className="profile-row" onClick={() => { setAttachOpen(false); if (onNewDraft) onNewDraft(); else void newSession(); }}>+ {t("chat", lang)}</button>
              <button type="button" className="profile-row" onClick={() => { setAttachOpen(false); fileRef.current?.click(); }}>{t("dropFiles", lang)}</button>
              {agents.length > 1 && (
                <>
                  <div className="section-label" style={{ paddingTop: 8 }}>{t("subagents", lang)}</div>
                  {agents.map((a) => (
                    <button type="button" key={a.id} className={`profile-row${activeAgent?.id === a.id ? " active" : ""}`} onClick={() => { setAttachOpen(false); void switchAgent(a); }}>
                      <span className={`pip${a.status === "running" ? " run" : " on"}`} /> {a.name}
                    </button>
                  ))}
                </>
              )}
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
            {imagesEl}
            <div className="composer-fade">
              <div className="composer-row">
                <div className="composer-menu">
                  <button
                    type="button"
                    className="icon-ghost"
                    aria-label={t("composerMore", lang)}
                    onClick={() => setAttachOpen((o) => !o)}
                  >
                    +
                  </button>
                  {voiceSupported && (
                    <button
                      type="button"
                      className={`icon-ghost mic-btn${listening ? " listening" : ""}`}
                      aria-label={t(listening ? "stop" : "voiceInput", lang)}
                      onClick={() => listening ? stopVoice() : startVoice()}
                    >
                      <Icon name="mic" size={15} />
                    </button>
                  )}
                </div>
                <textarea
                  ref={taRef}
                  className="composer-input"
                  rows={1}
                  value={input}
                  onChange={onType}
                  onPaste={onPaste}
                  onKeyDown={handleKeyDown}
                  placeholder={activeAgent ? t("message", lang) : t("pickProfile", lang)}
                  disabled={!canType}
                />
                <div className="composer-controls">
                  <div className="composer-profile">
                    {/* the chat's model: shown here, changed here (like /models) */}
                    <button
                      type="button"
                      ref={pillRef}
                      className={`model-pill${models && modelsFromPill ? " open" : ""}`}
                      aria-label={t("modelPick", lang)}
                      aria-haspopup="listbox"
                      aria-expanded={Boolean(models && modelsFromPill)}
                      title={`${t("modelPick", lang)}${chatModel?.source === "chat" ? ` · ${t("modelThisChat", lang)}` : ""}`}
                      onClick={() => { setAttachOpen(false); if (models) closeModels(); else void openModels(true); }}
                    >
                      <span className={`pip${activeAgent?.status === "running" || live ? " run" : activeAgent ? " on" : ""}`} />
                      <span className="pill-label">{modelLabel}</span>
                      {effort ? <span className="pill-sub pill-effort">· {t(`effort_${effort}`, lang)}</span> : modelProvider && <span className="pill-sub">· {modelProvider}</span>}
                      <span className="profile-caret">⌄</span>
                    </button>
                  </div>
                  {sending ? (
                    <button type="button" className="send-btn" onClick={() => void stop()} aria-label={t("stop", lang)}>■</button>
                  ) : (
                    <button type="submit" className="send-btn" disabled={!canSend} aria-label={t("send", lang)}>↑</button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </form>
      </div>
      {reviewEl}
    </div>
  );
}
