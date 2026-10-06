import { bridgeApp, resetConnection, type ConnState } from "./rpc";
import type { SSHTarget } from "./types";

// The app's connection: this computer, or a server running Rove. Only the
// desktop app can connect to a server (it runs the SSH tunnel).

type WailsRuntime = { EventsOn?: (name: string, cb: (data: unknown) => void) => (() => void) | void; EventsOff?: (name: string) => void };
const wailsRuntime = () => (window as unknown as { runtime?: WailsRuntime }).runtime;

export const canConnectRemote = () => Boolean(bridgeApp()?.ConnectRemote);

export async function connectionState(): Promise<ConnState> {
  const b = bridgeApp();
  if (!b?.Connection) return { status: "local" };
  try { return await b.Connection(); } catch { return { status: "local" }; }
}

// connectRemote sets up and connects to Rove on a server; the page reloads
// on success so every view reads from the server's daemon.
export async function connectRemote(target: SSHTarget): Promise<ConnState> {
  const b = bridgeApp();
  if (!b?.ConnectRemote) throw new Error("only the desktop app can run Rove on a server");
  const st = await b.ConnectRemote(JSON.stringify({ host: target.host, user: target.user ?? "", port: target.port ?? 0, keyPath: target.keyPath ?? "" }));
  if (st.status !== "connected") throw new Error(st.message || "connection failed");
  try { localStorage.setItem("aether.conn.host", st.host ?? target.host); } catch { /* private */ }
  resetConnection();
  window.location.reload();
  return st;
}

export async function disconnectRemote(): Promise<void> {
  const b = bridgeApp();
  if (!b?.DisconnectRemote) return;
  await b.DisconnectRemote();
  try { localStorage.removeItem("aether.conn.host"); } catch { /* private */ }
  resetConnection();
  window.location.reload();
}

function on(name: string, cb: (data: unknown) => void): () => void {
  const rt = wailsRuntime();
  if (!rt?.EventsOn) return () => {};
  const off = rt.EventsOn(name, cb);
  return typeof off === "function" ? off : () => rt.EventsOff?.(name);
}

// onConnection follows the connection; a tunnel back on a new port makes
// the page reach the daemon there without reloading.
export function onConnection(cb: (st: ConnState) => void): () => void {
  let lastHTTP = "";
  return on("rove:conn", (data) => {
    const st = data as ConnState;
    if (st.status === "connected" && st.http && lastHTTP && st.http !== lastHTTP) resetConnection();
    if (st.http) lastHTTP = st.http;
    cb(st);
  });
}

export function onConnectProgress(cb: (step: string) => void): () => void {
  return on("rove:conn-progress", (data) => cb(String(data)));
}
