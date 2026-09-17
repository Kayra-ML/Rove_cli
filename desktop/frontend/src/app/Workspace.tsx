import { useCallback, useEffect, useRef, useState, type MutableRefObject } from "react";
import { rpc } from "~/lib/rpc";
import type { Agent, Session, Workspace } from "~/lib/types";
import { useAgents, useSessions, useWorkspaces } from "~/hooks/useApi";
import { Icon } from "./Icons";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

interface PanelProps {
  active: Workspace | null;
  onSelect: (ws: Workspace) => void;
  onNewChat?: () => void;
}

export function WorkspacePanel({ active, onSelect, onNewChat }: PanelProps) {
  const { workspaces } = useWorkspaces();
  const { lang } = usePrefs();

  return (
    <div>
      <div className="section-label section-label-row">
        <span>{t("workspaces", lang)}</span>
        <button className="icon-btn" title={t("chat", lang)} onClick={() => onNewChat?.()}>
          <Icon name="plus" size={15} />
        </button>
      </div>
      {workspaces.map((ws: Workspace) => (
        <button
          key={ws.id}
          className={`nav-item${active?.id === ws.id ? " active" : ""}`}
          onClick={() => onSelect(ws)}
          title={ws.path}
        >
          <span style={{ display: "block" }}>{ws.name}</span>
          <span style={{ display: "block", fontSize: 11, color: "var(--muted)" }}>
            {ws.defaultBranch}
          </span>
        </button>
      ))}
      {workspaces.length === 0 && (
        <div style={{ color: "var(--faint)", padding: "4px 14px", fontSize: 12 }}>
          {t("noneYet", lang)}
        </div>
      )}
    </div>
  );
}

interface SessionPanelProps {
  workspaceId?: string;
  activeId: string | null;
  onSelect: (s: Session) => void;
  onCreated?: (s: Session) => void;
  createRef?: MutableRefObject<(() => Promise<Session | null>) | null>;
}

export function SessionPanel({ workspaceId, activeId, onSelect, onCreated, createRef }: SessionPanelProps) {
  const { agents } = useAgents();
  const { sessions, reload, loaded } = useSessions(workspaceId);
  const { lang } = usePrefs();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const creating = useRef(false);

  const ensureSession = useCallback(async (agent: Agent, forceNew = false) => {
    if (creating.current) return null;
    creating.current = true;
    try {
      if (!forceNew && sessions.length > 0) {
        const pick = sessions.find((s) => s.id === activeId) ?? sessions[0];
        onSelect(pick);
        return pick;
      }
      const sess = await rpc<Session>("session.create", {
        title: t("chat", lang),
        agentId: agent.id,
        workspaceId: workspaceId ?? "",
      });
      await reload();
      onSelect(sess);
      onCreated?.(sess);
      return sess;
    } finally {
      creating.current = false;
    }
  }, [workspaceId, reload, onSelect, onCreated, sessions, activeId, lang]);

  const add = useCallback(async () => {
    const agent = agents[0];
    if (!agent) return null;
    return ensureSession(agent, true);
  }, [agents, ensureSession]);

  const rename = useCallback(async (s: Session) => {
    setEditingId(s.id);
    setEditTitle(s.title || "");
  }, []);

  const commitRename = useCallback(async (s: Session) => {
    const next = editTitle.trim();
    setEditingId(null);
    if (!next || next === s.title) return;
    await rpc("session.rename", { id: s.id, title: next });
    await reload();
    if (activeId === s.id) onSelect({ ...s, title: next });
  }, [editTitle, reload, activeId, onSelect]);

  const remove = useCallback(async (s: Session) => {
    await rpc("session.delete", { id: s.id });
    await reload();
  }, [reload]);

  useEffect(() => {
    if (createRef) createRef.current = add;
  }, [createRef, add]);

  useEffect(() => {
    if (!loaded) return;
    if (sessions.length > 0) {
      if (!activeId || !sessions.some((s) => s.id === activeId)) {
        onSelect(sessions[0]);
      }
      return;
    }
    if (agents[0]) void ensureSession(agents[0], true);
  }, [loaded, sessions, agents, activeId, onSelect, ensureSession]);

  return (
    <div>
      <div className="section-label section-label-row">
        <span>{t("sessions", lang)}</span>
        <button className="icon-btn" title={t("chat", lang)} onClick={() => void add()}>
          <Icon name="plus" size={15} />
        </button>
      </div>
      {sessions.map((s) => (
        <div key={s.id} className={`nav-item${activeId === s.id ? " active" : ""}`} style={{ display: "flex", alignItems: "center", gap: 6 }}>
          {editingId === s.id ? (
            <input
              autoFocus
              value={editTitle}
              style={{ flex: 1, fontSize: 12 }}
              onChange={(e) => setEditTitle(e.target.value)}
              onBlur={() => void commitRename(s)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void commitRename(s);
                if (e.key === "Escape") setEditingId(null);
              }}
              onClick={(e) => e.stopPropagation()}
            />
          ) : (
            <button
              style={{ flex: 1, background: "none", border: 0, textAlign: "left", padding: 0 }}
              onClick={() => onSelect(s)}
              onDoubleClick={() => void rename(s)}
            >
              <span style={{ display: "block" }}>{s.title || t("chat", lang)}</span>
              <span style={{ display: "block", fontSize: 11, color: "var(--muted)" }}>
                {s.updatedAt ? new Date(s.updatedAt).toLocaleString() : ""}
              </span>
            </button>
          )}
          <button
            className="ghost"
            style={{ fontSize: 11, padding: "2px 6px" }}
            title={t("delete", lang)}
            onClick={(e) => { e.stopPropagation(); void remove(s); }}
          >
            ×
          </button>
        </div>
      ))}
      {sessions.length === 0 && (
        <div style={{ color: "var(--faint)", padding: "4px 14px", fontSize: 12 }}>
          {t("noneYet", lang)}
        </div>
      )}
    </div>
  );
}
