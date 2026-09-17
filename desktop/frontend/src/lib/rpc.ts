import type { RpcResponse } from "./types";

type WailsApp = {
  RPC: (method: string, paramsJSON: string) => Promise<string>;
  Token: () => Promise<string>;
  HTTPAddr: () => Promise<string>;
  PickFolder?: () => Promise<string>;
};

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

export async function rpc<T>(method: string, params: unknown = null): Promise<T> {
  await hydrateConnection().catch(() => undefined);
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

export function subscribeEvents(filter: string, onEvent: (ev: { type: string; topic?: string; payload?: unknown }) => void): () => void {
  const token = localStorage.getItem("aether.token") || "";
  const base = localStorage.getItem("aether.http") || HTTP_DEFAULT;
  const url = `${base}/events?token=${encodeURIComponent(token)}${filter ? `&filter=${encodeURIComponent(filter)}` : ""}`;
  let es: EventSource | null = null;
  let closed = false;
  let retry = 800;
  const attach = () => {
    if (closed) return;
    es = new EventSource(url);
    es.onmessage = (m) => {
      retry = 800;
      try {
        onEvent(JSON.parse(m.data));
      } catch {
        /* ignore malformed frames */
      }
    };
    es.onerror = () => {
      es?.close();
      es = null;
      if (closed) return;
      window.setTimeout(attach, retry);
      retry = Math.min(retry * 2, 8000);
    };
  };
  void hydrateConnection().then(() => attach()).catch(() => attach());
  return () => {
    closed = true;
    es?.close();
  };
}

export async function pickFolder(): Promise<string> {
  const bridge = wails();
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
