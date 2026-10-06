import { useCallback, useEffect, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type { WorkPlan } from "~/lib/types";

// A chat's current Teamwork plan, live: the daemon announces every change
// (planned, edited, a phase moving on, finished) on the chat's topic.
export function useTeamwork(sessionId: string | null | undefined) {
  const [plan, setPlan] = useState<WorkPlan | null>(null);
  const [loaded, setLoaded] = useState(false);
  const reload = useCallback(async () => {
    if (!sessionId) { setPlan(null); setLoaded(true); return; }
    try {
      setPlan(await rpc<WorkPlan | null>("teamwork.get", { sessionId }));
    } catch { /* keep */ } finally {
      setLoaded(true);
    }
  }, [sessionId]);
  useEffect(() => {
    setLoaded(false);
    void reload();
    if (!sessionId) return;
    return subscribeEvents(`session.${sessionId}`, (ev) => {
      if (ev.type === "teamwork.updated") void reload();
    });
  }, [sessionId, reload]);
  return { plan, setPlan, loaded, reload };
}
