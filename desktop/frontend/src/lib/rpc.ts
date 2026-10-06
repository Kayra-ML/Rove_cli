import type { RpcResponse } from "./types";

type WailsApp = {
  RPC: (method: string, paramsJSON: string) => Promise<string>;
  Token: () => Promise<string>;
  HTTPAddr: () => Promise<string>;
  PickFolder?: () => Promise<string>;
  DiscoverSSH?: () => Promise<DiscoveredSSH[]>;
  Connection?: () => Promise<ConnState>;
  ConnectRemote?: (targetJSON: string) => Promise<ConnState>;
  DisconnectRemote?: () => Promise<void>;
};

// Where the app is connected: this computer's daemon, or a server's through
// an SSH tunnel (the desktop app keeps that tunnel up).
export type ConnState = {
  status: "local" | "connecting" | "connected" | "reconnecting" | "error";
  host?: string;
  http?: string;
  token?: string;
  message?: string;
  installed?: boolean;
};

// An SSH server found on this computer (~/.ssh/config or known_hosts).
export type DiscoveredSSH = { alias?: string; hostName: string; user?: string; port?: number; keyPath?: string; source: "config" | "known_hosts" };

declare global {
  interface Window {
    go?: { main?: { App?: WailsApp } };
    runtime?: unknown;
  }
}

const HTTP_DEFAULT = "http://127.0.0.1:7420";

function wails(): WailsApp | undefined {
  return window.go?.main?.App;
}

let hydrateOnce: Promise<{ token: string; http: string }> | null = null;

async function httpCall<T>(method: string, params: unknown, token: string, base: string): Promise<T> {
  const res = await fetch(`${base}/rpc`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ method, params, token }),
  });
  if (!res.ok) throw new Error(`rpc ${method} HTTP ${res.status}`);
  const body = (await res.json()) as RpcResponse<T>;
  if (!body.ok) throw new Error(body.error || "rpc failed");
  return body.result as T;
}

// Reads that several parts of the app ask for at the same moment. Asking
// twice within a breath is the same question, so the answer is shared —
// on a server across an SSH tunnel every extra call is a whole round trip.
// Anything not listed here is sent as it comes; a call that changes
// something clears what was remembered, so nothing stale is served.
const SHARED_READS = new Set([
  "ping", "session.list", "session.model", "session.linked",
  "agent.list", "workspace.list", "workspace.rules", "usage.get", "model.list",
  "persona.catalog", "persona.get", "persona.badges", "edits.list",
  "permission.asks", "permission.list", "provider.list", "profile.list",
  "goal.list", "memory.list", "skill.list", "market.list",
  "automation.list", "automation.catalog", "terminal.panes", "terminal.layout.get",
]);

// How long an answer may be reused. Long enough to cover the whole opening
// burst, even when a slow link stretches it out, and short enough that no
// one reads a stale screen. Anything that changes something clears it
// anyway, so this only ever holds while nothing is happening.
// session.history is deliberately not shared: a reload is asked for when a
// reply lands, and it must be the state after it, not before.
const SHARE_MS = 1200;

const recent = new Map<string, { at: number; p: Promise<unknown> }>();

// Events that pour in while an answer is written. They carry their own
// payload and nothing re-reads because of them, so they leave the shared
// answers alone.
const STREAM_EVENTS = new Set(["message.delta", "tool.start", "tool.result", "terminal.data"]);

async function send<T>(method: string, params: unknown): Promise<T> {
  const bridge = wails();
  if (bridge) {
    const raw = await bridge.RPC(method, JSON.stringify(params));
    const body = JSON.parse(raw) as RpcResponse<T>;
    if (!body.ok) throw new Error(body.error || "rpc failed");
    return body.result as T;
  }
  const token = localStorage.getItem("aether.token") || "";
  const base = localStorage.getItem("aether.http") || HTTP_DEFAULT;
  return httpCall<T>(method, params, token, base);
}

export async function rpc<T>(method: string, params: unknown = null): Promise<T> {
  await hydrateConnection().catch(() => undefined);
  if (!SHARED_READS.has(method)) {
    const p = send<T>(method, params);
    // whatever it changed, the answers held here may no longer be true
    void p.then(() => recent.clear(), () => recent.clear());
    return p;
  }
  const key = `${method}\u0000${JSON.stringify(params ?? null)}`;
  const now = Date.now();
  const hit = recent.get(key);
  if (hit && now - hit.at < SHARE_MS) {
    // a later caller gets its own copy: one holder must not be able to
    // change what another is about to read
    return hit.p.then((v) => copy(v) as T);
  }
  const p = send<T>(method, params);
  recent.set(key, { at: now, p });
  void p.catch(() => recent.delete(key)); // a failure is not an answer
  for (const [k, v] of recent) {
    if (now - v.at > SHARE_MS) recent.delete(k);
  }
  return p;
}

function copy<T>(v: T): T {
  try {
    return structuredClone(v);
  } catch {
    return v;
  }
}

// forgetReads drops the shared answers: after something changes outside the
// usual path, the next ask goes to the daemon.
export function forgetReads() {
  recent.clear();
}

type DaemonEvent = { type: string; topic?: string; payload?: unknown };
type Sub = { filter: string; fn: (ev: DaemonEvent) => void };

// Every subscription shares ONE event stream. A stream per subscriber ran
// into the browser's (and WebKit's) limit of 6 connections per host: past
// that, new streams sat pending forever and their updates never arrived.
const subs = new Set<Sub>();
let stream: EventSource | null = null;
let retryMs = 800;
let retryTimer: number | undefined;

// eventMatches mirrors the daemon's eventbus filter: empty matches all;
// otherwise the event type, the topic, or a "prefix*" of the type.
export function eventMatches(filter: string, ev: DaemonEvent): boolean {
  if (!filter) return true;
  if (filter === ev.type || filter === ev.topic) return true;
  return filter.endsWith("*") && ev.type.startsWith(filter.slice(0, -1));
}

function openStream() {
  if (stream || subs.size === 0) return;
  const token = localStorage.getItem("aether.token") || "";
  const base = localStorage.getItem("aether.http") || HTTP_DEFAULT;
  const es = new EventSource(`${base}/events?token=${encodeURIComponent(token)}`);
  stream = es;
  es.onmessage = (m) => {
    retryMs = 800;
    let ev: DaemonEvent;
    try {
      ev = JSON.parse(m.data) as DaemonEvent;
    } catch {
      return; // malformed frame
    }
    // The daemon says something happened, and handlers are about to ask
    // what. A remembered answer is from before it happened, so it goes —
    // except for the ones that arrive many times a second while a reply is
    // being written, which change nothing that is asked for by name.
    if (!STREAM_EVENTS.has(ev.type)) recent.clear();
    for (const s of [...subs]) {
      if (!eventMatches(s.filter, ev)) continue;
      try {
        s.fn(ev);
      } catch (err) {
        console.error(err); // one bad handler must not starve the rest
      }
    }
  };
  es.onerror = () => {
    es.close();
    if (stream === es) stream = null;
    if (subs.size === 0) return;
    window.clearTimeout(retryTimer);
    retryTimer = window.setTimeout(openStream, retryMs);
    retryMs = Math.min(retryMs * 2, 8000);
  };
}

export function subscribeEvents(filter: string, onEvent: (ev: DaemonEvent) => void): () => void {
  const sub: Sub = { filter, fn: onEvent };
  subs.add(sub);
  void hydrateConnection().then(openStream, openStream);
  return () => {
    subs.delete(sub);
    if (subs.size === 0 && stream) {
      stream.close();
      stream = null;
    }
  };
}

// discoverSSH lists the SSH servers this computer knows. The desktop app
// looks on this machine itself; otherwise the (local) daemon does.
export async function discoverSSH(): Promise<DiscoveredSSH[]> {
  const bridge = wails();
  if (bridge?.DiscoverSSH) return (await bridge.DiscoverSSH()) ?? [];
  return (await rpc<DiscoveredSSH[]>("ssh.discover")) ?? [];
}

// resetConnection forgets the daemon address and reopens the event stream
// (the app moved to another daemon, or its tunnel came back on a new port).
export function resetConnection() {
  hydrateOnce = null;
  // another server has its own answers; none of the old ones hold
  recent.clear();
  if (stream) {
    stream.close();
    stream = null;
  }
  if (subs.size > 0) void hydrateConnection().then(openStream, openStream);
}

export function bridgeApp() {
  return wails();
}

// a picker for folders on a server, shown by the app (see FolderBrowser)
let remotePicker: (() => Promise<string>) | null = null;
export function setRemoteFolderPicker(fn: (() => Promise<string>) | null) {
  remotePicker = fn;
}

export async function pickFolder(): Promise<string> {
  const bridge = wails();
  // on a server, its folders: the native picker only sees this computer
  if (bridge?.Connection && remotePicker) {
    const st = await bridge.Connection().catch(() => null);
    if (st?.status === "connected" || st?.status === "reconnecting") return remotePicker();
  }
  if (bridge?.PickFolder) {
    return (await bridge.PickFolder()) || "";
  }
  return prompt("Workspace path:") ?? "";
}

export async function hydrateConnection(): Promise<{ token: string; http: string }> {
  if (hydrateOnce) return hydrateOnce;
  hydrateOnce = (async () => {
    const bridge = wails();
    if (bridge) {
      const token = await bridge.Token();
      const http = `http://${await bridge.HTTPAddr()}`;
      localStorage.setItem("aether.token", token);
      localStorage.setItem("aether.http", http);
      return { token, http };
    }
    const http = localStorage.getItem("aether.http") || HTTP_DEFAULT;
    const r = await fetch(`${http}/health`);
    if (!r.ok) throw new Error("unhealthy");
    const h = (await r.json()) as { ok?: boolean };
    if (!h.ok) throw new Error("unhealthy");
    return { token: localStorage.getItem("aether.token") || "", http };
  })().catch((err) => {
    hydrateOnce = null;
    throw err;
  });
  return hydrateOnce;
}
