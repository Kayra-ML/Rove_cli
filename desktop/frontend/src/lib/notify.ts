import { rpc, subscribeEvents } from "./rpc";
import { t, getLang } from "./i18n";
import { toast } from "./toast";

// Desktop notifications for work that finishes while the user is elsewhere:
// a goal, a Teamwork plan, a long reply. Only when the app is not in front.

const KEY = "aether.notify";
// a reply this long (ms) is worth a notification
const LONG_RUN_MS = 30_000;

export function notifyEnabled(): boolean {
  try { return localStorage.getItem(KEY) !== "off"; } catch { return true; }
}

export function setNotifyEnabled(on: boolean) {
  try { localStorage.setItem(KEY, on ? "on" : "off"); } catch { /* private */ }
}

type Bridge = { Notify?: (title: string, body: string) => Promise<void> };
const bridge = (): Bridge | undefined => (window as unknown as { go?: { main?: { App?: Bridge } } }).go?.main?.App;

const away = () => document.hidden || !document.hasFocus();

// notify shows a system notification when the app is not in front: through
// the desktop app when it runs there, else the browser's.
export async function notify(title: string, body: string, force = false): Promise<boolean> {
  if (!notifyEnabled() || (!force && !away())) return false;
  const b = bridge();
  if (b?.Notify) {
    try { await b.Notify(title, body); return true; } catch { /* fall through */ }
  }
  if (typeof Notification === "undefined") return false;
  if (Notification.permission === "default") {
    try { await Notification.requestPermission(); } catch { /* ignore */ }
  }
  if (Notification.permission !== "granted") return false;
  new Notification(title, { body });
  return true;
}

// watchFinishes subscribes to what is worth a notification. It returns the
// unsubscribe.
export function watchFinishes(): () => void {
  const started = new Map<string, number>();
  const titleOf = async (sessionId: string) => {
    try {
      const list = await rpc<{ id: string; title: string }[]>("session.list", { workspaceId: "" });
      return list.find((s) => s.id === sessionId)?.title ?? "";
    } catch { return ""; }
  };
  const mark = (ev: { payload?: unknown }) => {
    const sid = (ev.payload as { sessionId?: string } | undefined)?.sessionId;
    if (sid && !started.has(sid)) started.set(sid, Date.now());
  };
  const offs = [
    subscribeEvents("message.delta", mark),
    subscribeEvents("tool.start", mark),
    subscribeEvents("run.done", (ev) => {
      const p = (ev.payload ?? {}) as { sessionId?: string; error?: string };
      if (!p.sessionId) return;
      const since = started.get(p.sessionId);
      started.delete(p.sessionId);
      if (since === undefined || Date.now() - since < LONG_RUN_MS) return;
      void titleOf(p.sessionId).then((title) =>
        notify(p.error ? t("notifyRunFailed", getLang()) : t("notifyRunDone", getLang()), title || p.error || ""));
    }),
    subscribeEvents("goal.updated", (ev) => {
      // published per goal and per chat: take the goal's copy only
      if (!(ev as { topic?: string }).topic?.startsWith("goal.")) return;
      const p = (ev.payload ?? {}) as { id?: string; status?: string };
      if (!p.id || !["done", "blocked"].includes(p.status ?? "")) return;
      void rpc<{ title: string; lastVerdict?: { reason?: string } }>("goal.get", { id: p.id }).then((g) =>
        notify(t(p.status === "done" ? "notifyGoalDone" : "notifyGoalBlocked", getLang()), p.status === "done" ? g.title : `${g.title} — ${g.lastVerdict?.reason ?? ""}`)).catch(() => {});
    }),
    // an agent of the Agent space finished a task the user gave it: a
    // notification when away, a toast when here
    subscribeEvents("staff.updated", (ev) => {
      const p = (ev.payload ?? {}) as { title?: string; status?: string; from?: string; name?: string };
      if (p.from || !["done", "failed"].includes(p.status ?? "")) return;
      const head = t(p.status === "done" ? "notifyTaskDone" : "notifyTaskFailed", getLang()).replace("{name}", p.name ?? "");
      void notify(head, p.title ?? "").then((shown) => { if (!shown) toast(`${head}: ${p.title ?? ""}`, p.status === "done" ? "ok" : "err"); });
    }),
    subscribeEvents("teamwork.updated", (ev) => {
      const p = (ev.payload ?? {}) as { sessionId?: string; status?: string };
      if (!p.sessionId || !["done", "failed"].includes(p.status ?? "")) return;
      void titleOf(p.sessionId).then((title) =>
        notify(t(p.status === "done" ? "notifyPlanDone" : "notifyPlanFailed", getLang()), title));
    }),
  ];
  return () => offs.forEach((f) => f());
}
