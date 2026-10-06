import { useCallback, useEffect, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import { pollWhileVisible } from "~/lib/poll";
import type { Session, StaffHandoff, StaffMember, StaffMonitor, StaffSchedule, StaffWatch } from "~/lib/types";

// useStaff is the Agent space's team as the daemon reports it: who is
// working on what, their latest tasks and their notes, and the automations
// that hand them work — the chats they watch and their schedules. It follows
// the tasks' events and every run's end (a chat with an agent finishing
// changes its state too), with a slow poll behind them.
export function useStaff() {
  const [members, setMembers] = useState<StaffMember[] | null>(null);
  const [watches, setWatches] = useState<StaffWatch[]>([]);
  const [schedules, setSchedules] = useState<StaffSchedule[]>([]);
  const [monitors, setMonitors] = useState<StaffMonitor[]>([]);
  const [handoffs, setHandoffs] = useState<StaffHandoff[]>([]);
  const [chats, setChats] = useState<Map<string, string>>(new Map());
  const reload = useCallback(async () => {
    try {
      // the daemon may send an empty list as null: every list is made one
      const list = (await rpc<StaffMember[]>("staff.members")) ?? [];
      setMembers(list.map((m) => ({ ...m, tasks: m.tasks ?? [], notes: m.notes ?? [] })));
    } catch { /* keep */ }
    try {
      const [w, s, c, m, h] = await Promise.all([
        rpc<StaffWatch[]>("staff.watches"),
        rpc<StaffSchedule[]>("staff.schedules"),
        rpc<Session[]>("session.list", { workspaceId: "" }),
        rpc<StaffMonitor[]>("staff.monitors"),
        rpc<StaffHandoff[]>("staff.handoffs"),
      ]);
      // anything but a list (an older daemon, a failed call) is none
      setWatches(Array.isArray(w) ? w : []);
      setSchedules(Array.isArray(s) ? s : []);
      setMonitors(Array.isArray(m) ? m : []);
      setHandoffs(Array.isArray(h) ? h : []);
      setChats(new Map((Array.isArray(c) ? c : []).map((x) => [x.id, x.title])));
    } catch { /* keep */ }
  }, []);
  useEffect(() => {
    void reload();
    const stop = pollWhileVisible(() => void reload(), 8000);
    const on = () => void reload();
    // a run starting is seen at its first tool call; those come in bursts,
    // so they ask for one reload a moment later rather than one each
    let soon: ReturnType<typeof setTimeout> | null = null;
    const later = () => { if (!soon) soon = setTimeout(() => { soon = null; void reload(); }, 1200); };
    window.addEventListener("rove:profiles", on);
    const offs = [
      subscribeEvents("staff.updated", on), subscribeEvents("run.done", on), subscribeEvents("ctxmap.changed", on),
      subscribeEvents("tool.start", later),
    ];
    return () => {
      stop();
      if (soon) clearTimeout(soon);
      window.removeEventListener("rove:profiles", on);
      offs.forEach((f) => f());
    };
  }, [reload]);
  return { members, watches, schedules, monitors, handoffs, chats, reload };
}
