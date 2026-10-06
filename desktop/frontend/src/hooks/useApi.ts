import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import type {
  Agent, AgentProfile, Goal, InstalledSkill, Message,
  Provider, Session, Workspace,
} from "~/lib/types";

export function useAgents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const reload = useCallback(async () => {
    try { setAgents((await rpc<Agent[]>("agent.list")) ?? []); } catch { setAgents((prev) => prev); }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { agents, reload };
}

export function useSessions(workspaceId?: string) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loaded, setLoaded] = useState(false);
  const reload = useCallback(async () => {
    try {
      setSessions((await rpc<Session[]>("session.list", { workspaceId: workspaceId ?? "" })) ?? []);
    } catch {
      setSessions([]);
    } finally {
      setLoaded(true);
    }
  }, [workspaceId]);
  useEffect(() => { setLoaded(false); void reload(); }, [reload]);
  return { sessions, reload, loaded };
}

export function useHistory(sessionId: string | null) {
  const [messages, setMessages] = useState<Message[]>([]);
  const reload = useCallback(async () => {
    if (!sessionId) { setMessages([]); return; }
    try { setMessages((await rpc<Message[]>("session.history", { sessionId })) ?? []); } catch { /* keep */ }
  }, [sessionId]);
  useEffect(() => { void reload(); }, [reload]);
  return { messages, reload };
}

export function useGoals() {
  const [goals, setGoals] = useState<Goal[]>([]);
  const reload = useCallback(async () => {
    try { setGoals((await rpc<Goal[]>("goal.list")) ?? []); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { goals, reload };
}

export function useWorkspaces() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const reload = useCallback(async () => {
    try { setWorkspaces((await rpc<Workspace[]>("workspace.list")) ?? []); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { workspaces, reload };
}

export function useSkills() {
  const [skills, setSkills] = useState<InstalledSkill[]>([]);
  const reload = useCallback(async () => {
    try { setSkills((await rpc<InstalledSkill[]>("skill.list")) ?? []); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { skills, reload };
}

export function useProviders() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const reload = useCallback(async () => {
    try { setProviders((await rpc<Provider[]>("provider.list")) ?? []); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { providers, reload };
}

export function useProfiles() {
  const [profiles, setProfiles] = useState<AgentProfile[]>([]);
  const reload = useCallback(async () => {
    try { setProfiles(await rpc<AgentProfile[]>("profile.list") ?? []); } catch { /* keep */ }
  }, []);
  useEffect(() => {
    void reload();
    // Every caller of this hook has its own copy of `profiles`; a save in
    // one (e.g. Settings → Profiles) would otherwise leave others (e.g. the
    // sidebar's Agents tab) showing a stale list until they happen to remount.
    const on = () => void reload();
    window.addEventListener("rove:profiles", on);
    return () => window.removeEventListener("rove:profiles", on);
  }, [reload]);
  return { profiles, reload };
}
