import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { CtxNode, Message, Relay, Session, SessionLink, Workspace } from "~/lib/types";
import { buildSessionContext, sharedFiles, type SessionContext } from "~/lib/sessionContext";
import { useWorkspaces } from "~/hooks/useApi";
import { usePersonaBadges } from "~/hooks/usePersona";
import { usePrefs } from "~/hooks/usePrefs";
import { toast } from "~/lib/toast";
import { t, type Lang } from "~/lib/i18n";
import { CARD_H, CARD_W, anchors, at, cable, cardRect, edge, fitView, freeSpot, hitCard, slide, spread, type Pt, type Rect } from "~/lib/cables";
import { useProfiles } from "~/hooks/useApi";
import { useStaff } from "~/hooks/useStaff";
import { AgentMark } from "./AgentMark";
import { spaceOf, type Space } from "~/lib/spaces";

// how close to a card's border (in screen pixels) a press starts a cable
const EDGE_PX = 14;

// An Agent-space agent stands on the map as a card of its own, keyed
// "agent:<profile id>"; a cable from a chat to it is a watch.
const AGENT = "agent:";
const isAgent = (id: string) => id.startsWith(AGENT);
const profileOf = (id: string) => id.slice(AGENT.length);

// A handoff: when one agent finishes, the other takes the work on
// (internal/staff/handoff.go).
export type Handoff = { id: string; fromId: string; toId: string; instruction: string; dailyLimit: number; enabled: boolean; color?: string };

// A watch as the daemon keeps it (internal/staff/watch.go).
export type Watch = {
  id: string;
  sessionId: string;
  profileId: string;
  instruction: string;
  filter?: string;
  dailyLimit: number;
  enabled: boolean;
  color?: string;
};

const DRAG_MIME = "application/x-rove-session";
const CAM_KEY = "aether.ctxmap.cam";
const GRID = 24;

type Cam = { x: number; y: number; k: number };
type Gesture =
  | { kind: "pan"; sx: number; sy: number; cam: Cam }
  | { kind: "card"; id: string; dx: number; dy: number; moved: boolean }
  | { kind: "cable"; from: string; side: 1 | -1 };
type Pulse = { key: string; linkId: string; from: string; to: string; status: string };

interface Props {
  onOpenSession?: (s: Session) => void;
  // Session Map shows one space's chats (Office or Chat) at a time
  space?: Space;
  // shown at the top of the sidebar (the Automation page's pickers)
  sideTop?: ReactNode;
  // open a chat's own context map
  onShowContext?: (id: string) => void;
}

function loadCam(): Cam | null {
  try {
    const raw = localStorage.getItem(CAM_KEY);
    const c = raw ? (JSON.parse(raw) as Cam) : null;
    return c && Number.isFinite(c.k) ? c : null;
  } catch {
    return null;
  }
}

export function ContextMap({ onOpenSession, space, sideTop, onShowContext }: Props) {
  const { lang } = usePrefs();
  const stageRef = useRef<HTMLDivElement | null>(null);
  const [allSessions, setSessions] = useState<Session[]>([]);
  const { workspaces } = useWorkspaces();
  const { badgeFor } = usePersonaBadges();
  // only the chosen space's chats: their cards, cables and list
  const sessions = useMemo(
    () => (space ? allSessions.filter((s) => spaceOf(s, badgeFor(s.id)) === space) : allSessions),
    [allSessions, badgeFor, space],
  );
  const [nodes, setNodes] = useState<Map<string, Pt>>(new Map());
  const [links, setLinks] = useState<SessionLink[]>([]);
  const [relays, setRelays] = useState<Relay[]>([]);
  const [cam, setCam] = useState<Cam>(() => loadCam() ?? { x: 80, y: 80, k: 1 });
  const [gesture, setGesture] = useState<Gesture | null>(null);
  const [pointer, setPointer] = useState<Pt | null>(null);
  const [selLink, setSelLink] = useState<string | null>(null);
  const [selCard, setSelCard] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [typing, setTyping] = useState<Record<string, number>>({});
  const [pulses, setPulses] = useState<Pulse[]>([]);
  const [dropHint, setDropHint] = useState(false);
  const { profiles } = useProfiles();
  const { members } = useStaff();
  const [watches, setWatches] = useState<Watch[]>([]);
  const [selWatch, setSelWatch] = useState<string | null>(null);
  const [handoffs, setHandoffs] = useState<Handoff[]>([]);
  const [selHandoff, setSelHandoff] = useState<string | null>(null);
  const profileById = useMemo(() => new Map(profiles.map((p) => [p.id, p])), [profiles]);

  const sessionById = useMemo(() => new Map(sessions.map((s) => [s.id, s])), [sessions]);
  const wsById = useMemo(() => new Map(workspaces.map((w: Workspace) => [w.id, w])), [workspaces]);
  const rects = useMemo(() => {
    const m = new Map<string, Rect>();
    for (const [id, p] of nodes) {
      if (sessionById.has(id) || (isAgent(id) && profileById.has(profileOf(id)))) m.set(id, cardRect(p));
    }
    return m;
  }, [nodes, sessionById, profileById]);

  const loadSessions = useCallback(async () => {
    try {
      setSessions((await rpc<Session[]>("session.list", { workspaceId: "" })) ?? []);
    } catch { /* keep */ }
  }, []);

  const loadMap = useCallback(async () => {
    try {
      const g = await rpc<{ nodes: CtxNode[]; links: SessionLink[] }>("ctxmap.get");
      setNodes(new Map((g.nodes ?? []).map((n) => [n.sessionId, { x: n.x, y: n.y }])));
      setLinks(g.links ?? []);
    } catch (e) {
      toast(e instanceof Error ? e.message : "ctxmap failed", "err");
    }
  }, []);

  const loadWatches = useCallback(async () => {
    try { setWatches((await rpc<Watch[]>("staff.watches")) ?? []); } catch { /* keep */ }
    try {
      const h = await rpc<Handoff[]>("staff.handoffs");
      setHandoffs(Array.isArray(h) ? h : []);
    } catch { /* keep */ }
  }, []);

  const loadRelays = useCallback(async () => {
    try {
      setRelays((await rpc<Relay[]>("ctxmap.relays", { limit: 30 })) ?? []);
    } catch { /* keep */ }
  }, []);

  useEffect(() => {
    void loadSessions();
    void loadMap();
    void loadRelays();
    void loadWatches();
    const id = window.setInterval(() => void loadSessions(), 15000);
    return () => window.clearInterval(id);
  }, [loadSessions, loadMap, loadRelays, loadWatches]);

  useEffect(() => {
    try { localStorage.setItem(CAM_KEY, JSON.stringify(cam)); } catch { /* private mode */ }
  }, [cam]);

  // live: relays pulse along their cable, other clients' edits reload the
  // canvas, and streaming sessions show as typing
  useEffect(() => {
    const offRelay = subscribeEvents("ctxmap.relay", (ev) => {
      const r = ev.payload as Relay | undefined;
      if (!r?.id) return;
      setRelays((list) => [r, ...list.filter((x) => x.id !== r.id)].slice(0, 30));
      if (r.linkId && (r.status === "running" || r.status === "skipped" || r.status === "failed")) {
        const key = `${r.id}:${r.status}`;
        setPulses((p) => [...p.filter((x) => x.key !== key), { key, linkId: r.linkId!, from: r.from, to: r.to, status: r.status }]);
        window.setTimeout(() => setPulses((p) => p.filter((x) => x.key !== key)), 1800);
      }
    });
    const offMap = subscribeEvents("ctxmap.changed", () => { void loadMap(); void loadWatches(); });
    const offDelta = subscribeEvents("message.delta", (ev) => {
      const sid = (ev.payload as { sessionId?: string } | undefined)?.sessionId;
      if (sid) setTyping((m) => ({ ...m, [sid]: Date.now() }));
    });
    return () => { offRelay(); offMap(); offDelta(); };
  }, [loadMap, loadWatches]);

  useEffect(() => {
    const id = window.setInterval(() => {
      setTyping((m) => {
        const now = Date.now();
        const next = Object.fromEntries(Object.entries(m).filter(([, ts]) => now - ts < 2000));
        return Object.keys(next).length === Object.keys(m).length ? m : next;
      });
    }, 1000);
    return () => window.clearInterval(id);
  }, []);

  // ── coordinates ───────────────────────────────────────────────────────────

  const toWorld = useCallback((clientX: number, clientY: number): Pt => {
    const r = stageRef.current?.getBoundingClientRect();
    const sx = clientX - (r?.left ?? 0);
    const sy = clientY - (r?.top ?? 0);
    return { x: (sx - cam.x) / cam.k, y: (sy - cam.y) / cam.k };
  }, [cam]);

  const place = useCallback(async (id: string, p: Pt) => {
    const snapped = { x: Math.round(p.x / (GRID / 2)) * (GRID / 2), y: Math.round(p.y / (GRID / 2)) * (GRID / 2) };
    setNodes((m) => new Map(m).set(id, snapped));
    try {
      await rpc("ctxmap.place", { sessionId: id, ...snapped });
    } catch (e) {
      toast(e instanceof Error ? e.message : "place failed", "err");
    }
  }, []);

  const fit = useCallback(() => {
    const r = stageRef.current?.getBoundingClientRect();
    if (!r) return;
    setCam(fitView([...rects.values()], r.width, r.height));
  }, [rects]);

  // The view is remembered across both spaces, so opening one can land on
  // empty canvas where the other's cards were. When none of this space's
  // cards is in sight, frame them once instead of showing a blank board.
  const framed = useRef(false);
  useEffect(() => {
    if (framed.current || rects.size === 0) return;
    const r = stageRef.current?.getBoundingClientRect();
    if (!r || r.width === 0) return;
    framed.current = true;
    const inSight = [...rects.values()].some((c) => {
      const x = c.x * cam.k + cam.x;
      const y = c.y * cam.k + cam.y;
      return x < r.width && x + c.w * cam.k > 0 && y < r.height && y + c.h * cam.k > 0;
    });
    if (!inSight) fit();
  }, [rects, cam, fit]);

  const zoomBy = useCallback((factor: number, at?: Pt) => {
    const r = stageRef.current?.getBoundingClientRect();
    const sx = at?.x ?? (r ? r.width / 2 : 0);
    const sy = at?.y ?? (r ? r.height / 2 : 0);
    setCam((c) => {
      const k = Math.min(2, Math.max(0.25, c.k * factor));
      const wx = (sx - c.x) / c.k;
      const wy = (sy - c.y) / c.k;
      return { k, x: sx - wx * k, y: sy - wy * k };
    });
  }, []);

  // ── pointer gestures ─────────────────────────────────────────────────────

  const onStageDown = (e: React.PointerEvent) => {
    if (e.button !== 0 && e.button !== 1) return;
    setSelLink(null);
    setSelCard(null);
    setSelWatch(null);
    setSelHandoff(null);
    setGesture({ kind: "pan", sx: e.clientX, sy: e.clientY, cam });
  };

  // A cable starts anywhere along a card's edge, or anywhere on it with ⌥
  // held — not only from the two small ports.
  const onEdge = (id: string, w: Pt) => {
    const rc = rects.get(id);
    if (!rc) return false;
    const band = EDGE_PX / cam.k;
    return Math.min(w.x - rc.x, rc.x + rc.w - w.x, w.y - rc.y, rc.y + rc.h - w.y) <= band;
  };
  const [edgeOver, setEdgeOver] = useState<string | null>(null);

  const onCardDown = (e: React.PointerEvent, id: string) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    const w = toWorld(e.clientX, e.clientY);
    if (e.altKey || onEdge(id, w)) {
      const rc = rects.get(id)!;
      setGesture({ kind: "cable", from: id, side: w.x >= rc.x + rc.w / 2 ? 1 : -1 });
      setPointer(w);
      return;
    }
    const p = nodes.get(id)!;
    setSelCard(id);
    setSelLink(null);
    setGesture({ kind: "card", id, dx: w.x - p.x, dy: w.y - p.y, moved: false });
  };

  // while a cable is drawn, the card it would land on lights up
  const dropOn = gesture?.kind === "cable" && pointer ? hitCard(pointer, rects, gesture.from) : null;

  const onPortDown = (e: React.PointerEvent, id: string, side: 1 | -1) => {
    e.stopPropagation();
    setGesture({ kind: "cable", from: id, side });
    setPointer(toWorld(e.clientX, e.clientY));
  };

  const onMove = (e: React.PointerEvent) => {
    if (!gesture) return;
    if (gesture.kind === "pan") {
      setCam({ ...gesture.cam, x: gesture.cam.x + e.clientX - gesture.sx, y: gesture.cam.y + e.clientY - gesture.sy });
    } else if (gesture.kind === "card") {
      const w = toWorld(e.clientX, e.clientY);
      // a card slides along the others but never goes into one
      setNodes((m) => {
        const prev = m.get(gesture.id);
        const want = { x: w.x - gesture.dx, y: w.y - gesture.dy };
        const others = [...rects.entries()].filter(([k]) => k !== gesture.id).map(([, r]) => r);
        return new Map(m).set(gesture.id, prev ? slide(prev, want, others) : want);
      });
      if (!gesture.moved) setGesture({ ...gesture, moved: true });
    } else {
      setPointer(toWorld(e.clientX, e.clientY));
    }
  };

  const onUp = async (e: React.PointerEvent) => {
    const g = gesture;
    setGesture(null);
    if (!g) return;
    if (g.kind === "card" && g.moved) {
      const p = nodes.get(g.id);
      if (p) await place(g.id, p);
    } else if (g.kind === "cable") {
      setPointer(null);
      const target = hitCard(toWorld(e.clientX, e.clientY), rects, g.from);
      if (!target) return;
      // chat ↔ agent: the agent watches the chat, whichever way it was drawn
      if (isAgent(g.from) || isAgent(target)) {
        // agent → agent: the first's finished work goes on to the second
        if (isAgent(g.from) && isAgent(target)) {
          try {
            const h = await rpc<Handoff>("staff.handoffSave", { fromId: profileOf(g.from), toId: profileOf(target) });
            setHandoffs((hs) => (hs.some((x) => x.id === h.id) ? hs : [...hs, h]));
            setSelHandoff(h.id);
            setSelWatch(null);
            setSelLink(null);
          } catch (err) {
            toast(err instanceof Error ? err.message : "handoff failed", "err");
          }
          return;
        }
        const agentId = profileOf(isAgent(g.from) ? g.from : target);
        const sessionId = isAgent(g.from) ? target : g.from;
        try {
          const w = await rpc<Watch>("staff.watchSave", { sessionId, profileId: agentId });
          setWatches((ws) => (ws.some((x) => x.id === w.id) ? ws : [...ws, w]));
          setSelWatch(w.id);
          setSelLink(null);
        } catch (err) {
          toast(err instanceof Error ? err.message : "watch failed", "err");
        }
        return;
      }
      try {
        const l = await rpc<SessionLink>("ctxmap.link", { sessionA: g.from, sessionB: target });
        setLinks((ls) => (ls.some((x) => x.id === l.id) ? ls : [...ls, l]));
        setSelLink(l.id);
      } catch (err) {
        toast(err instanceof Error ? err.message : "link failed", "err");
      }
    }
  };

  const onWheel = (e: React.WheelEvent) => {
    const r = stageRef.current?.getBoundingClientRect();
    if (!r) return;
    zoomBy(Math.exp(-e.deltaY * 0.0015), { x: e.clientX - r.left, y: e.clientY - r.top });
  };

  // ── drag from the sidebar ────────────────────────────────────────────────

  // Dragging is not the only way on: a click puts the card in the middle of
  // what you are looking at (drag and drop is unreliable in some webviews).
  const addToView = (id: string) => {
    const r = stageRef.current?.getBoundingClientRect();
    if (!r) return;
    const w = toWorld(r.left + r.width / 2, r.top + r.height / 2);
    void place(id, freeSpot({ x: w.x - CARD_W / 2, y: w.y - CARD_H / 2 }, [...rects.values()]));
  };

  // frame the canvas once the cards just added are in
  const refit = useRef(false);
  useEffect(() => {
    if (refit.current && rects.size > 0) {
      refit.current = false;
      fit();
    }
  }, [rects, fit]);

  // every listed chat not yet on the canvas, laid out in a grid to the
  // right of what is there
  const addAll = () => {
    const todo = listed.filter((s) => !rects.has(s.id));
    const taken = [...rects.values()];
    const x0 = taken.length ? Math.max(...taken.map((r) => r.x + r.w)) + 48 : 0;
    const y0 = taken.length ? Math.min(...taken.map((r) => r.y)) : 0;
    const cols = Math.max(1, Math.ceil(Math.sqrt(todo.length)));
    todo.forEach((s, i) => {
      void place(s.id, { x: x0 + (i % cols) * (CARD_W + 48), y: y0 + Math.floor(i / cols) * (CARD_H + 40) });
    });
    refit.current = true;
  };

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDropHint(false);
    const id = e.dataTransfer.getData(DRAG_MIME);
    if (!id) return;
    const w = toWorld(e.clientX, e.clientY);
    const others = [...rects.entries()].filter(([k]) => k !== id).map(([, v]) => v);
    void place(id, freeSpot({ x: w.x - CARD_W / 2, y: w.y - CARD_H / 2 }, others));
  };

  // ── link edits ───────────────────────────────────────────────────────────

  const updateLink = async (id: string, patch: Partial<SessionLink>) => {
    setLinks((ls) => ls.map((l) => (l.id === id ? { ...l, ...patch } : l)));
    try {
      await rpc("ctxmap.updateLink", { id, ...patch });
    } catch (e) {
      toast(e instanceof Error ? e.message : "update failed", "err");
      void loadMap();
    }
  };

  const deleteLink = async (id: string) => {
    setLinks((ls) => ls.filter((l) => l.id !== id));
    setSelLink(null);
    await rpc("ctxmap.unlink", { id }).catch(() => loadMap());
  };

  const saveWatch = async (w: Watch) => {
    setWatches((ws) => ws.map((x) => (x.id === w.id ? w : x)));
    try {
      await rpc("staff.watchSave", w);
    } catch (e) {
      toast(e instanceof Error ? e.message : "update failed", "err");
      void loadWatches();
    }
  };
  const saveHandoff = async (h: Handoff) => {
    setHandoffs((hs) => hs.map((x) => (x.id === h.id ? h : x)));
    try { await rpc("staff.handoffSave", h); } catch (e) { toast(e instanceof Error ? e.message : "update failed", "err"); void loadWatches(); }
  };
  const deleteHandoff = async (id: string) => {
    setHandoffs((hs) => hs.filter((x) => x.id !== id));
    setSelHandoff(null);
    await rpc("staff.handoffDelete", { id }).catch(() => loadWatches());
  };
  const deleteWatch = async (id: string) => {
    setWatches((ws) => ws.filter((x) => x.id !== id));
    setSelWatch(null);
    await rpc("staff.watchDelete", { id }).catch(() => loadWatches());
  };

  const removeCard = async (id: string) => {
    // a watch hidden off the map would keep working unseen: its cable goes
    // with the card
    if (isAgent(id)) {
      const pid = profileOf(id);
      const handed = handoffs.filter((h) => h.fromId === pid || h.toId === pid);
      if (handed.length) {
        setHandoffs((hs) => hs.filter((h) => !handed.includes(h)));
        for (const h of handed) void rpc("staff.handoffDelete", { id: h.id }).catch(() => loadWatches());
      }
    }
    const gone = watches.filter((w) => (isAgent(id) ? w.profileId === profileOf(id) : w.sessionId === id));
    if (gone.length) {
      setWatches((ws) => ws.filter((w) => !gone.includes(w)));
      if (gone.some((w) => w.id === selWatch)) setSelWatch(null);
      for (const w of gone) void rpc("staff.watchDelete", { id: w.id }).catch(() => loadWatches());
    }
    setNodes((m) => { const n = new Map(m); n.delete(id); return n; });
    setLinks((ls) => ls.filter((l) => l.sessionA !== id && l.sessionB !== id));
    await rpc("ctxmap.remove", { sessionId: id }).catch(() => loadMap());
  };

  const focusCard = (id: string) => {
    const p = nodes.get(id);
    const r = stageRef.current?.getBoundingClientRect();
    if (!p || !r) return;
    setSelCard(id);
    setCam((c) => ({ ...c, x: r.width / 2 - (p.x + CARD_W / 2) * c.k, y: r.height / 2 - (p.y + CARD_H / 2) * c.k }));
  };

  // ── render helpers ───────────────────────────────────────────────────────

  // A cable only does something between two different project folders: the
  // same folder has nothing to mirror, and a chat on the disk root has no
  // project to find a counterpart in. Say so where the cable is edited.
  const pathOf = (id: string) => wsById.get(allSessions.find((s) => s.id === id)?.workspaceId ?? "")?.path ?? "";
  const cableWarn = (l: SessionLink) => {
    const a = pathOf(l.sessionA);
    const b = pathOf(l.sessionB);
    // a message by hand always works; only the automatic notices need two
    // different project folders to compare
    if (!l.auto) return "";
    if (!a || a === "/" || !b || b === "/") return "ctxWarnNoProject";
    return a === b ? "ctxWarnSameProject" : "";
  };
  // What each chat on the canvas is carrying: its requests, the files it
  // touched, its size. Read from the history, so any chat has one.
  const [ctxs, setCtxs] = useState<Map<string, SessionContext>>(new Map());
  const loadCtx = useCallback(async (id: string, title: string) => {
    try {
      const h = await rpc<Message[]>("session.history", { sessionId: id });
      setCtxs((m) => new Map(m).set(id, buildSessionContext(h ?? [], id, title)));
    } catch { /* keep */ }
  }, []);
  const placedKey = [...rects.keys()].sort().join(",");
  useEffect(() => {
    for (const id of rects.keys()) if (!isAgent(id) && !ctxs.has(id)) void loadCtx(id, sessionById.get(id)?.title ?? "");
  }, [placedKey, loadCtx]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => subscribeEvents("message.done", (ev) => {
    const id = (ev.payload as { sessionId?: string } | undefined)?.sessionId;
    if (id && rects.has(id)) void loadCtx(id, sessionById.get(id)?.title ?? "");
  }), [rects, sessionById, loadCtx]);
  // Chats that touched the same files share context, cable or not: a dashed
  // line says so, with the files on hover.
  const shared = useMemo(() => {
    const ids = [...rects.keys()].filter((id) => ctxs.has(id));
    const cabled = new Set(links.map((l) => [l.sessionA, l.sessionB].sort().join(">")));
    const out: { a: string; b: string; files: string[] }[] = [];
    for (let i = 0; i < ids.length; i++) {
      for (let j = i + 1; j < ids.length; j++) {
        if (cabled.has([ids[i], ids[j]].sort().join(">"))) continue;
        const files = sharedFiles(ctxs.get(ids[i])!, ctxs.get(ids[j])!);
        if (files.length) out.push({ a: ids[i], b: ids[j], files });
      }
    }
    return out;
  }, [rects, ctxs, links]);
  const ctxLine = (id: string) => {
    const c = ctxs.get(id);
    if (!c) return "";
    const files = [...c.items.values()].filter((it) => it.kind === "file").length;
    const tok = c.liveTokens >= 1000 ? `${(c.liveTokens / 1000).toFixed(1)}k` : String(c.liveTokens);
    return `${c.turns.length} ${t("ctxTurns", lang)} · ${files} ${t("ctxFilesShort", lang)} · ≈${tok} token`;
  };
  // a relay can come from the other space's chat: name it from every session
  const titleOf = (id: string) => allSessions.find((s) => s.id === id)?.title || id.slice(0, 6);
  // the disk root names no project; the chat list leaves it out too
  const wsName = (s?: Session) => {
    const w = s ? wsById.get(s.workspaceId) : undefined;
    return w && w.path !== "/" ? w.name : "";
  };
  const visibleLinks = links.filter((l) => rects.has(l.sessionA) && rects.has(l.sessionB));
  const selected = links.find((l) => l.id === selLink) ?? null;
  const visibleWatches = watches.filter((w) => rects.has(w.sessionId) && rects.has(AGENT + w.profileId));
  const watchSel = watches.find((w) => w.id === selWatch) ?? null;
  const visibleHandoffs = handoffs.filter((h) => rects.has(AGENT + h.fromId) && rects.has(AGENT + h.toId));
  const handoffSel = handoffs.find((h) => h.id === selHandoff) ?? null;
  // Where every cable leaves and lands: level or upright between aligned
  // cards, and spread along a card's edge where several leave one spot.
  const ends = useMemo(() => {
    const raw: { key: string; a: string; b: string }[] = [
      ...visibleLinks.map((l) => ({ key: `l:${l.id}`, a: l.sessionA, b: l.sessionB })),
      ...visibleWatches.map((w) => ({ key: `w:${w.id}`, a: w.sessionId, b: AGENT + w.profileId })),
      ...visibleHandoffs.map((h) => ({ key: `h:${h.id}`, a: AGENT + h.fromId, b: AGENT + h.toId })),
    ];
    const pts: [string, Pt][] = [];
    for (const r of raw) {
      const an = anchors(rects.get(r.a)!, rects.get(r.b)!);
      pts.push([r.a, an.from], [r.b, an.to]);
    }
    const moved = spread(pts, rects);
    const m = new Map<string, { from: Pt; to: Pt }>();
    raw.forEach((r, i) => m.set(r.key, { from: moved[2 * i], to: moved[2 * i + 1] }));
    return m;
  }, [visibleLinks, visibleWatches, visibleHandoffs, rects]);
  const stateOf = (pid: string) => members?.find((m) => m.profile.id === pid);
  const lastRelay = useMemo(() => {
    const m = new Map<string, Relay>();
    for (const r of relays) if (!m.has(r.to)) m.set(r.to, r);
    return m;
  }, [relays]);

  const q = filter.trim().toLowerCase();
  const listed = sessions.filter((s) => !q || s.title.toLowerCase().includes(q) || wsName(s).toLowerCase().includes(q));
  const agentsListed = profiles.filter((p) => !q || p.name.toLowerCase().includes(q) || (p.title ?? "").toLowerCase().includes(q));

  const gridStyle: React.CSSProperties = {
    backgroundSize: `${GRID * cam.k}px ${GRID * cam.k}px, ${GRID * cam.k}px ${GRID * cam.k}px, ${GRID * 5 * cam.k}px ${GRID * 5 * cam.k}px, ${GRID * 5 * cam.k}px ${GRID * 5 * cam.k}px`,
    backgroundPosition: `${cam.x}px ${cam.y}px`,
  };

  return (
    <div className="cmap">
      <div
        ref={stageRef}
        className={`cmap-stage${gesture?.kind === "pan" ? " panning" : ""}${dropHint ? " drop" : ""}`}
        style={gridStyle}
        onPointerDown={onStageDown}
        onPointerMove={onMove}
        onPointerUp={(e) => void onUp(e)}
        onPointerLeave={(e) => { if (gesture?.kind !== "cable") void onUp(e); }}
        onWheel={onWheel}
        onDragOver={(e) => { if (e.dataTransfer.types.includes(DRAG_MIME)) { e.preventDefault(); setDropHint(true); } }}
        onDragLeave={() => setDropHint(false)}
        onDrop={onDrop}
      >
        <div className="cmap-world" style={{ transform: `translate(${cam.x}px, ${cam.y}px) scale(${cam.k})` }}>
          <svg className="cmap-wires" width="1" height="1">
            {visibleLinks.map((l) => {
              const en = ends.get(`l:${l.id}`)!;
              const c = cable(en.from, en.to);
              const mid = at(c, 0.5);
              const tint = l.color ? { stroke: `var(--cc-${l.color})` } : undefined;
              // the path always runs A → B, so the arrow follows it for a2b
              // and turns around for b2a; both ways gets a double chevron
              const flowsForward = l.direction === "a2b" ? true : l.direction === "b2a" ? false : null;
              const on = l.id === selLink;
              return (
                <g key={l.id} className={`cmap-wire${l.auto ? " auto" : ""}${on ? " on" : ""}`}>
                  <path d={c.d} className="cmap-wire-shadow" />
                  <path d={c.d} className="cmap-wire-core" style={tint} />
                  <path
                    d={c.d}
                    className="cmap-wire-hit"
                    onPointerDown={(e) => { e.stopPropagation(); setSelLink(l.id); setSelCard(null); }}
                  />
                  <g transform={`translate(${mid.x} ${mid.y}) rotate(${flowsForward === false ? mid.angle + 180 : mid.angle})`} className="cmap-wire-mark" style={tint}>
                    {flowsForward === null ? (
                      <>
                        <path d="M -9 -5 L -3 0 L -9 5" />
                        <path d="M 9 -5 L 3 0 L 9 5" />
                      </>
                    ) : (
                      <path d="M -5 -6 L 5 0 L -5 6" />
                    )}
                  </g>
                  {l.label && (
                    <text x={mid.x} y={mid.y - 12} className="cmap-wire-label">{l.label}</text>
                  )}
                  {pulses.filter((p) => p.linkId === l.id).map((p) => {
                    // the pulse runs the cable from the side it came from
                    const pc = p.from === l.sessionA ? c : cable(en.to, en.from);
                    return (
                      <circle key={p.key} r={6} className={`cmap-pulse ${p.status}`}>
                        <animateMotion dur="1.4s" repeatCount="1" path={pc.d} fill="freeze" />
                      </circle>
                    );
                  })}
                </g>
              );
            })}
            {visibleWatches.map((w) => {
              // a watch runs from the chat to the agent watching it
              const en = ends.get(`w:${w.id}`)!;
              const c = cable(en.from, en.to);
              const mid = at(c, 0.5);
              const on = w.id === selWatch;
              const tint = w.color && w.enabled ? { stroke: `var(--cc-${w.color})` } : undefined;
              const label = w.instruction ? (w.instruction.length > 36 ? `${w.instruction.slice(0, 35)}…` : w.instruction) : t("ctxWatching", lang);
              return (
                <g key={w.id} className={`cmap-watch${w.enabled ? "" : " off"}${on ? " on" : ""}`} data-watch={w.id}>
                  <path d={c.d} className="cmap-watch-line" style={tint} />
                  <path d={c.d} className="cmap-wire-hit" onPointerDown={(e) => { e.stopPropagation(); setSelWatch(w.id); setSelLink(null); setSelCard(null); }} />
                  <g transform={`translate(${mid.x} ${mid.y}) rotate(${mid.angle})`} className="cmap-watch-mark" style={tint}>
                    <circle r={9} />
                    <path d="M -3 -4 L 2 0 L -3 4" />
                  </g>
                  <text x={mid.x} y={mid.y - 14} className="cmap-wire-label">{label}</text>
                </g>
              );
            })}
            {visibleHandoffs.map((h) => {
              // a handoff runs from the agent who finishes to the one who goes on
              const en = ends.get(`h:${h.id}`)!;
              const c = cable(en.from, en.to);
              const mid = at(c, 0.5);
              const tint = h.color && h.enabled ? { stroke: `var(--cc-${h.color})` } : undefined;
              const label = h.instruction ? (h.instruction.length > 36 ? `${h.instruction.slice(0, 35)}…` : h.instruction) : t("ctxHandsOn", lang);
              return (
                <g key={h.id} className={`cmap-watch cmap-handoff${h.enabled ? "" : " off"}${h.id === selHandoff ? " on" : ""}`} data-handoff={h.id}>
                  <path d={c.d} className="cmap-watch-line" style={tint} />
                  <path d={c.d} className="cmap-wire-hit" onPointerDown={(e) => { e.stopPropagation(); setSelHandoff(h.id); setSelWatch(null); setSelLink(null); setSelCard(null); }} />
                  <g transform={`translate(${mid.x} ${mid.y}) rotate(${mid.angle})`} className="cmap-watch-mark" style={tint}>
                    <circle r={9} />
                    <path d="M -4 -4 L 1 0 L -4 4 M 0 -4 L 5 0 L 0 4" />
                  </g>
                  <text x={mid.x} y={mid.y - 14} className="cmap-wire-label">{label}</text>
                </g>
              );
            })}
            {shared.map((x) => {
              const a = rects.get(x.a)!;
              const b = rects.get(x.b)!;
              const an = anchors(a, b);
              const c = cable(an.from, an.to, an.fromSide, an.toSide);
              const mid = at(c, 0.5);
              return (
                <g key={`${x.a}>${x.b}`} className="cmap-shared">
                  <title>{`${t("ctxShared", lang)}:\n${x.files.join("\n")}`}</title>
                  <path d={c.d} />
                  <text x={mid.x} y={mid.y - 8} className="cmap-wire-label">{x.files.length} {t("ctxSharedFiles", lang)}</text>
                </g>
              );
            })}
            {gesture?.kind === "cable" && pointer && rects.get(gesture.from) && (() => {
              // while drawing, the cable runs straight from the card's edge
              // to the pointer, leaving the card towards it
              const a = rects.get(gesture.from)!;
              const from = edge(a, { x: a.x + a.w / 2, y: a.y + a.h / 2 }, pointer);
              const c = cable(from, pointer);
              return <path d={c.d} className="cmap-wire-draft" />;
            })()}
          </svg>

          {[...rects.entries()].map(([id, rc]) => {
            if (isAgent(id)) {
              const p = profileById.get(profileOf(id))!;
              const m = stateOf(p.id);
              const state = m?.state ?? "idle";
              const watching = watches.filter((w) => w.profileId === p.id).length;
              return (
                <div
                  key={id}
                  data-agent={p.id}
                  className={`cmap-card cmap-agent ${state}${selCard === id ? " sel" : ""}${edgeOver === id ? " edge" : ""}${dropOn === id ? " drop" : ""}`}
                  style={{ left: rc.x, top: rc.y, width: rc.w, height: rc.h }}
                  onPointerDown={(e) => onCardDown(e, id)}
                  onPointerMove={(e) => {
                    if (gesture) return;
                    const near = e.altKey || onEdge(id, toWorld(e.clientX, e.clientY));
                    if (near !== (edgeOver === id)) setEdgeOver(near ? id : null);
                  }}
                  onPointerLeave={() => { if (edgeOver === id) setEdgeOver(null); }}
                >
                  <div className="cmap-card-head">
                    <AgentMark mark={p.mark} color={p.color} seed={p.id} character={p.characterId} size={20} />
                    <span className="cmap-card-title">{p.name}</span>
                    <span className={`staff-dot ${state}`} title={t(`staffState_${state}`, lang)} />
                    <button
                      type="button"
                      className="cmap-card-x"
                      title={t("ctxRemoveCard", lang)}
                      onPointerDown={(e) => e.stopPropagation()}
                      onClick={() => void removeCard(id)}
                    >×</button>
                  </div>
                  <div className="cmap-card-sub">{p.title || t("staffNoTitle", lang)}</div>
                  <div className="cmap-card-foot">
                    {state === "working" && m?.now ? <span className="cmap-agent-now">{m.now}</span>
                      : <span className="map-muted">{watching ? t("ctxWatchingN", lang).replace("{n}", String(watching)) : t("ctxAgentHint", lang)}</span>}
                  </div>
                </div>
              );
            }
            const s = sessionById.get(id)!;
            const busy = Boolean(typing[id]);
            const lr = lastRelay.get(id);
            return (
              <div
                key={id}
                className={`cmap-card${selCard === id ? " sel" : ""}${busy ? " busy" : ""}${edgeOver === id ? " edge" : ""}${dropOn === id ? " drop" : ""}`}
                style={{ left: rc.x, top: rc.y, width: rc.w, height: rc.h }}
                onPointerDown={(e) => onCardDown(e, id)}
                onPointerMove={(e) => {
                  if (gesture) return;
                  const near = e.altKey || onEdge(id, toWorld(e.clientX, e.clientY));
                  if (near !== (edgeOver === id)) setEdgeOver(near ? id : null);
                }}
                onPointerLeave={() => { if (edgeOver === id) setEdgeOver(null); }}
                onDoubleClick={() => onOpenSession?.(s)}
                title={t("ctxOpen", lang)}
              >
                <div className="cmap-card-head">
                  <span className={`pip${busy ? " run" : " on"}`} />
                  {(() => {
                    // an Agent-space chat carries its agent's logo
                    const b = badgeFor(id);
                    return b ? <AgentMark mark={b.mark} color={b.color} seed={b.profileId || b.characterId || id} character={b.characterId} size={18} title={b.name} /> : null;
                  })()}
                  <span className="cmap-card-title">{s.title || id}</span>
                  <button
                    type="button"
                    className="cmap-card-x"
                    title={t("ctxRemoveCard", lang)}
                    onPointerDown={(e) => e.stopPropagation()}
                    onClick={() => void removeCard(id)}
                  >×</button>
                </div>
                <div className="cmap-card-sub">{[ctxLine(id) || wsName(s), badgeFor(id)?.name].filter(Boolean).join(" · ") || "—"}</div>
                <div className="cmap-card-foot">
                  {busy ? t("ctxTyping", lang) : lr ? <RelayChip r={lr} lang={lang} /> : null}
                  {onShowContext && (
                    <button type="button" className="ghost map-mini cmap-card-ctx" onPointerDown={(e) => e.stopPropagation()} onClick={() => onShowContext(id)}>
                      {t("ctxShowContext", lang)} →
                    </button>
                  )}
                </div>
                <span className="cmap-port left" onPointerDown={(e) => onPortDown(e, id, -1)} />
                <span className="cmap-port right" onPointerDown={(e) => onPortDown(e, id, 1)} />
              </div>
            );
          })}
        </div>

        {rects.size === 0 && <div className="cmap-empty">{t("ctxEmpty", lang)}</div>}

        <div className="cmap-zoom" onPointerDown={(e) => e.stopPropagation()}>
          <button type="button" onClick={() => zoomBy(1.2)}>+</button>
          <button type="button" onClick={() => zoomBy(1 / 1.2)}>−</button>
          <button type="button" onClick={fit}>{t("ctxFit", lang)}</button>
          <span className="cmap-zoom-k">{Math.round(cam.k * 100)}%</span>
        </div>

        {handoffSel && (
          <HandoffPanel
            key={handoffSel.id}
            handoff={handoffSel}
            from={profileById.get(handoffSel.fromId)?.name ?? ""}
            to={profileById.get(handoffSel.toId)?.name ?? ""}
            lang={lang}
            onChange={(h) => void saveHandoff(h)}
            onDelete={() => void deleteHandoff(handoffSel.id)}
            onClose={() => setSelHandoff(null)}
          />
        )}
        {watchSel && !handoffSel && (
          <WatchPanel
            key={watchSel.id}
            watch={watchSel}
            chat={titleOf(watchSel.sessionId)}
            agent={profileById.get(watchSel.profileId)?.name ?? ""}
            lang={lang}
            onChange={(w) => void saveWatch(w)}
            onDelete={() => void deleteWatch(watchSel.id)}
            onClose={() => setSelWatch(null)}
          />
        )}
        {selected && !watchSel && !handoffSel && (
          <LinkPanel
            link={selected}
            titleOf={titleOf}
            warn={cableWarn(selected)}
            lang={lang}
            onChange={(patch) => void updateLink(selected.id, patch)}
            onDelete={() => void deleteLink(selected.id)}
            onClose={() => setSelLink(null)}
          />
        )}
      </div>

      <aside className="cmap-side">
        {sideTop}
        <div className="section-label section-label-row">
          <span>{space ? t("convs", lang) : t("ctxSessions", lang)}</span>
          {listed.some((s) => !rects.has(s.id)) && (
            <button type="button" className="ghost map-mini" onClick={addAll}>+ {t("ctxAddAll", lang)}</button>
          )}
        </div>
        <div className="cmap-side-search">
          <input value={filter} placeholder={t("ctxSearch", lang)} onChange={(e) => setFilter(e.target.value)} />
        </div>
        <div className="cmap-list">
          {listed.length === 0 && <div className="map-muted">{t("ctxNoSessions", lang)}</div>}
          {listed.map((s) => {
            const on = rects.has(s.id);
            return (
              <div
                key={s.id}
                className={`cmap-item${on ? " placed" : ""}`}
                draggable
                onDragStart={(e) => { e.dataTransfer.setData(DRAG_MIME, s.id); e.dataTransfer.effectAllowed = "move"; }}
                onClick={() => (on ? focusCard(s.id) : addToView(s.id))}
                title={on ? undefined : t("ctxClickAdd", lang)}
                onDoubleClick={() => onOpenSession?.(s)}
              >
                <div className="cmap-item-title">
                  {(() => {
                    const b = badgeFor(s.id);
                    return b ? <AgentMark mark={b.mark} color={b.color} seed={b.profileId || b.characterId || s.id} character={b.characterId} size={16} title={b.name} /> : null;
                  })()}
                  <span>{s.title || s.id}</span>
                </div>
                <div className="cmap-item-sub">
                  <span>{badgeFor(s.id)?.name ?? wsName(s)}</span>
                  {on ? <span className="cmap-badge">{t("ctxOnMap", lang)}</span> : <span>{ago(s.updatedAt)}</span>}
                </div>
              </div>
            );
          })}
        </div>
        {profiles.length > 0 && (
          <>
            <div className="section-label">{t("ctxAgents", lang)}</div>
            <div className="cmap-list cmap-agents">
              {agentsListed.map((p) => {
                const key = AGENT + p.id;
                const on = rects.has(key);
                return (
                  <div
                    key={p.id}
                    className={`cmap-item cmap-agent-item${on ? " placed" : ""}`}
                    draggable
                    onDragStart={(e) => { e.dataTransfer.setData(DRAG_MIME, key); e.dataTransfer.effectAllowed = "move"; }}
                    onClick={() => (on ? focusCard(key) : addToView(key))}
                    title={on ? undefined : t("ctxClickAdd", lang)}
                  >
                    <AgentMark mark={p.mark} color={p.color} seed={p.id} character={p.characterId} size={18} />
                    <div className="cmap-item-text">
                      <div className="cmap-item-title">{p.name}</div>
                      <div className="cmap-item-sub">
                        <span>{p.title || t("staffNoTitle", lang)}</span>
                        {on && <span className="cmap-badge">{t("ctxOnMap", lang)}</span>}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </>
        )}
        <div className="section-label">{t("ctxFlow", lang)}</div>
        <div className="cmap-flow">
          {relays.length === 0 && <div className="map-muted">{t("ctxNoFlow", lang)}</div>}
          {relays.slice(0, 12).map((r) => (
            <div key={r.id} className="cmap-flow-row" title={[r.summary, r.error, ...(r.matches ?? [])].filter(Boolean).join("\n")}>
              <div className="cmap-flow-top">
                <span className="cmap-flow-path">{titleOf(r.from)} → {titleOf(r.to)}</span>
                <RelayChip r={r} lang={lang} />
              </div>
              {r.files && r.files.length > 0 && (
                <div className="cmap-flow-files">{r.files.slice(0, 3).join(", ")}{r.files.length > 3 ? ` +${r.files.length - 3}` : ""}</div>
              )}
            </div>
          ))}
        </div>
      </aside>
    </div>
  );
}

function RelayChip({ r, lang }: { r: Relay; lang: Lang }) {
  const key: Record<string, string> = {
    queued: "ctxQueued", waiting: "ctxWaiting", running: "ctxRunning", done: "ctxDone", skipped: "ctxSkipped", failed: "ctxFailed",
  };
  const label = t(key[r.status] ?? r.status, lang);
  const cost = r.status === "skipped" ? "" : r.tokens ? ` · ≈${r.tokens} tok` : "";
  return <span className={`cmap-chip ${r.status}`}>{label}{cost}</span>;
}

// The colours a cable may take (internal/types CableColors), in the
// validated categorical order; each is drawn in its theme's step.
export const CABLE_COLORS = ["blue", "orange", "aqua", "yellow", "magenta", "green", "violet", "red"] as const;

function Swatches({ value, onPick, lang }: { value?: string; onPick: (c: string) => void; lang: Lang }) {
  return (
    <div className="cmap-swatches" role="radiogroup" aria-label={t("ctxColor", lang)}>
      <button type="button" role="radio" aria-checked={!value} className={`cmap-swatch none${!value ? " on" : ""}`} title={t("ctxColorDefault", lang)} onClick={() => onPick("")} />
      {CABLE_COLORS.map((c) => (
        <button key={c} type="button" role="radio" aria-checked={value === c} aria-label={t(`color_${c}`, lang)} title={t(`color_${c}`, lang)}
          className={`cmap-swatch${value === c ? " on" : ""}`} style={{ background: `var(--cc-${c})` }} onClick={() => onPick(c)} />
      ))}
    </div>
  );
}

// HandoffPanel edits a handoff: what the second agent does with the first's
// finished work, how many a day, on or off, its colour.
function HandoffPanel({ handoff, from, to, lang, onChange, onDelete, onClose }: {
  handoff: Handoff;
  from: string;
  to: string;
  lang: Lang;
  onChange: (h: Handoff) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const L = (k: string) => t(k, lang);
  const [instruction, setInstruction] = useState(handoff.instruction);
  const [limit, setLimit] = useState(String(handoff.dailyLimit || 10));
  const [arm, setArm] = useState(false);
  const commit = (patch: Partial<Handoff>) => onChange({ ...handoff, ...patch });
  return (
    <div className="cmap-panel cmap-watch-panel" role="dialog" aria-label={L("ctxHandoff")} onPointerDown={(e) => e.stopPropagation()} onWheel={(e) => e.stopPropagation()}>
      <div className="cmap-panel-head">
        <strong>{from} → {to}</strong>
        <button type="button" className="icon-btn" onClick={onClose}>×</button>
      </div>
      <p className="map-muted cmap-watch-lead">{L("ctxHandoffLead").replace("{from}", from).replace("{to}", to)}</p>
      <label className="map-label" htmlFor="h-ins">{L("ctxHandoffInstruction")}</label>
      <textarea id="h-ins" rows={3} value={instruction} placeholder={L("ctxHandoffInstructionPh")}
        onChange={(e) => setInstruction(e.target.value)} onBlur={() => instruction !== handoff.instruction && commit({ instruction })} />
      <label className="map-label">{L("ctxColor")}</label>
      <Swatches value={handoff.color} lang={lang} onPick={(color) => commit({ color })} />
      <label className="map-label" htmlFor="h-limit">{L("ctxWatchLimit")}</label>
      <input id="h-limit" type="number" min={1} max={200} value={limit}
        onChange={(e) => setLimit(e.target.value)}
        onBlur={() => { const n = Math.max(1, Math.min(200, Number(limit) || 10)); setLimit(String(n)); if (n !== handoff.dailyLimit) commit({ dailyLimit: n }); }} />
      <div className="cmap-seg">
        <button type="button" className={handoff.enabled ? "on" : ""} aria-pressed={handoff.enabled} onClick={() => commit({ enabled: true })}>{L("ctxWatchOn")}</button>
        <button type="button" className={!handoff.enabled ? "on" : ""} aria-pressed={!handoff.enabled} onClick={() => commit({ enabled: false })}>{L("ctxWatchOff")}</button>
      </div>
      <button type="button" className={`ghost danger${arm ? " armed" : ""}`} onBlur={() => setArm(false)}
        onClick={() => { if (arm) onDelete(); else setArm(true); }}>
        {arm ? L("ctxWatchDeleteSure") : L("ctxHandoffDelete")}
      </button>
    </div>
  );
}

// WatchPanel edits a watch: what the agent is to do when the chat changes
// files, which files count, how many times a day at most, and on or off.
function WatchPanel({ watch, chat, agent, lang, onChange, onDelete, onClose }: {
  watch: Watch;
  chat: string;
  agent: string;
  lang: Lang;
  onChange: (w: Watch) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const L = (k: string) => t(k, lang);
  const [instruction, setInstruction] = useState(watch.instruction);
  const [filter, setFilter] = useState(watch.filter ?? "");
  const [limit, setLimit] = useState(String(watch.dailyLimit || 10));
  const [arm, setArm] = useState(false);
  const commit = (patch: Partial<Watch>) => onChange({ ...watch, ...patch });
  return (
    <div className="cmap-panel cmap-watch-panel" role="dialog" aria-label={L("ctxWatch")} onPointerDown={(e) => e.stopPropagation()} onWheel={(e) => e.stopPropagation()}>
      <div className="cmap-panel-head">
        <strong>{agent} {L("ctxWatches")} · {chat}</strong>
        <button type="button" className="icon-btn" onClick={onClose}>×</button>
      </div>
      <p className="map-muted cmap-watch-lead">{L("ctxWatchLead").replace("{agent}", agent).replace("{chat}", chat)}</p>
      <label className="map-label" htmlFor="w-ins">{L("ctxWatchInstruction")}</label>
      <textarea id="w-ins" rows={3} value={instruction} placeholder={L("ctxWatchInstructionPh")}
        onChange={(e) => setInstruction(e.target.value)} onBlur={() => instruction !== watch.instruction && commit({ instruction })} />
      <label className="map-label">{L("ctxColor")}</label>
      <Swatches value={watch.color} lang={lang} onPick={(color) => commit({ color })} />
      <label className="map-label" htmlFor="w-filter">{L("ctxWatchFilter")}</label>
      <input id="w-filter" value={filter} placeholder={L("ctxWatchFilterPh")}
        onChange={(e) => setFilter(e.target.value)} onBlur={() => filter !== (watch.filter ?? "") && commit({ filter })} />
      <label className="map-label" htmlFor="w-limit">{L("ctxWatchLimit")}</label>
      <input id="w-limit" type="number" min={1} max={200} value={limit}
        onChange={(e) => setLimit(e.target.value)}
        onBlur={() => { const n = Math.max(1, Math.min(200, Number(limit) || 10)); setLimit(String(n)); if (n !== watch.dailyLimit) commit({ dailyLimit: n }); }} />
      <div className="cmap-seg">
        <button type="button" className={watch.enabled ? "on" : ""} aria-pressed={watch.enabled} onClick={() => commit({ enabled: true })}>{L("ctxWatchOn")}</button>
        <button type="button" className={!watch.enabled ? "on" : ""} aria-pressed={!watch.enabled} onClick={() => commit({ enabled: false })}>{L("ctxWatchOff")}</button>
      </div>
      <button type="button" className={`ghost danger${arm ? " armed" : ""}`} onBlur={() => setArm(false)}
        onClick={() => { if (arm) onDelete(); else setArm(true); }}>
        {arm ? L("ctxWatchDeleteSure") : L("ctxWatchDelete")}
      </button>
    </div>
  );
}

function LinkPanel({
  link, titleOf, warn, lang, onChange, onDelete, onClose,
}: {
  link: SessionLink;
  titleOf: (id: string) => string;
  // an i18n key when this cable cannot mirror anything, else ""
  warn: string;
  lang: Lang;
  onChange: (p: Partial<SessionLink>) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const L = (k: string) => t(k, lang);
  const [label, setLabel] = useState(link.label ?? "");
  const [msg, setMsg] = useState("");
  const [fromA, setFromA] = useState(true);
  useEffect(() => setLabel(link.label ?? ""), [link.id, link.label]);
  const a = titleOf(link.sessionA);
  const b = titleOf(link.sessionB);
  const send = async () => {
    const content = msg.trim();
    if (!content) return;
    try {
      await rpc("ctxmap.send", { from: fromA ? link.sessionA : link.sessionB, to: fromA ? link.sessionB : link.sessionA, content });
      setMsg("");
      toast(L("ctxSent"), "ok");
    } catch (e) {
      toast(e instanceof Error ? e.message : "send failed", "err");
    }
  };
  return (
    <div className="cmap-panel" onPointerDown={(e) => e.stopPropagation()} onWheel={(e) => e.stopPropagation()}>
      <div className="cmap-panel-head">
        <strong>{L("ctxCable")}: {a} ⇄ {b}</strong>
        <button type="button" className="icon-btn" onClick={onClose}>×</button>
      </div>
      {warn && <div className="cmap-warn">{L(warn)}</div>}
      <label className="map-label">{L("ctxLabel")}</label>
      <input value={label} placeholder={L("ctxLabelHint")} onChange={(e) => setLabel(e.target.value)} onBlur={() => label !== (link.label ?? "") && onChange({ label })} />
      <label className="map-label">{L("ctxColor")}</label>
      <Swatches value={link.color} lang={lang} onPick={(color) => onChange({ color })} />
      <label className="map-label">{L("ctxDirection")}</label>
      <div className="cmap-seg">
        <button type="button" className={link.direction === "both" || !link.direction ? "on" : ""} onClick={() => onChange({ direction: "both" })}>{L("ctxBoth")}</button>
        <button type="button" className={link.direction === "a2b" ? "on" : ""} onClick={() => onChange({ direction: "a2b" })}>{a} →</button>
        <button type="button" className={link.direction === "b2a" ? "on" : ""} onClick={() => onChange({ direction: "b2a" })}>← {b}</button>
      </div>
      <label className="cmap-check">
        <input type="checkbox" checked={Boolean(link.auto)} onChange={(e) => onChange({ auto: e.target.checked })} />
        {L("ctxAuto")}
      </label>
      {link.auto && (
        <>
          <label className="map-label">{L("ctxMode")}</label>
          <div className="cmap-seg">
            <button type="button" className={link.mode !== "always" ? "on" : ""} onClick={() => onChange({ mode: "smart" })}>{L("ctxSmart")}</button>
            <button type="button" className={link.mode === "always" ? "on" : ""} onClick={() => onChange({ mode: "always" })}>{L("ctxAlways")}</button>
          </div>
          <div className="cmap-hint">{link.mode === "always" ? L("ctxAlwaysHint") : L("ctxSmartHint")}</div>
        </>
      )}
      <label className="map-label">{L("ctxMessage")}</label>
      <div className="cmap-seg">
        <button type="button" className={fromA ? "on" : ""} onClick={() => setFromA(true)}>{a} → {b}</button>
        <button type="button" className={!fromA ? "on" : ""} onClick={() => setFromA(false)}>{b} → {a}</button>
      </div>
      <textarea rows={2} value={msg} onChange={(e) => setMsg(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) void send(); }} />
      <div className="cmap-panel-actions">
        <button type="button" className="danger-btn" onClick={onDelete}>{L("ctxDelete")}</button>
        <button type="button" className="primary" disabled={!msg.trim()} onClick={() => void send()}>{L("ctxSend")}</button>
      </div>
    </div>
  );
}

function ago(iso?: string): string {
  if (!iso) return "";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (!Number.isFinite(s) || s < 0) return "";
  if (s < 60) return "şimdi";
  if (s < 3600) return `${Math.floor(s / 60)} dk`;
  if (s < 86400) return `${Math.floor(s / 3600)} sa`;
  return `${Math.floor(s / 86400)} g`;
}
