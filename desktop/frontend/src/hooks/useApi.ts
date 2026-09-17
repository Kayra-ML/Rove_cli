import { useCallback, useEffect, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import type {
  Agent, Card, Goal, InstalledSkill, Message,
  Provider, Session, Workspace,
} from "~/lib/types";

function quiet<T>(fallback: T) {
  return (err: unknown): T => {
    void err;
    return fallback;
  };
}

export function useAgents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const reload = useCallback(async () => {
    try { setAgents(await rpc<Agent[]>("agent.list")); } catch { setAgents((prev) => prev); }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { agents, reload };
}

export function useSessions(workspaceId?: string) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loaded, setLoaded] = useState(false);
  const reload = useCallback(async () => {
    try {
      setSessions(await rpc<Session[]>("session.list", { workspaceId: workspaceId ?? "" }));
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
    try { setMessages(await rpc<Message[]>("session.history", { sessionId })); } catch { /* keep */ }
  }, [sessionId]);
  useEffect(() => { void reload(); }, [reload]);
  return { messages, reload };
}

export function useCards(workspaceId?: string) {
  const [cards, setCards] = useState<Card[]>([]);
  const reload = useCallback(async () => {
    try { setCards(await rpc<Card[]>("card.list", { workspaceId: workspaceId ?? "" })); } catch { /* keep */ }
  }, [workspaceId]);
  useEffect(() => { void reload(); }, [reload]);
  return { cards, reload };
}

export function useGoals() {
  const [goals, setGoals] = useState<Goal[]>([]);
  const reload = useCallback(async () => {
    try { setGoals(await rpc<Goal[]>("goal.list")); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { goals, reload };
}

export function useWorkspaces() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const reload = useCallback(async () => {
    try { setWorkspaces(await rpc<Workspace[]>("workspace.list")); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { workspaces, reload };
}

export function useSkills() {
  const [skills, setSkills] = useState<InstalledSkill[]>([]);
  const reload = useCallback(async () => {
    try { setSkills(await rpc<InstalledSkill[]>("skill.list")); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { skills, reload };
}

export function useProviders() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const reload = useCallback(async () => {
    try { setProviders(await rpc<Provider[]>("provider.list")); } catch { /* keep */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { providers, reload };
}

export function useTerminals() {
  const reload = useCallback(async () => {
    try { return await rpc<unknown[]>("terminal.list"); } catch { return quiet<unknown[]>([])(null); }
  }, []);
  return { reload };
}

export function useSSE(filter: string, onEvent: (ev: unknown) => void) {
  useEffect(() => {
    const unsub = subscribeEvents(filter, onEvent as Parameters<typeof subscribeEvents>[1]);
    return unsub;
  }, [filter, onEvent]);
}
