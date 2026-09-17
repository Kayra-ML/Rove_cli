import { rpc } from "~/lib/rpc";
import type { Card, Goal, Session } from "~/lib/types";
import { useAgents, useCards, useGoals } from "~/hooks/useApi";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { Icon } from "./Icons";

interface Props {
  session: Session | null;
  workspaceId?: string;
  workspacePath?: string;
  activity: Record<string, string>;
  onSelectCard?: (c: Card) => void;
  onOpenRoster?: () => void;
}

export function SessionRail({
  session,
  workspaceId,
  workspacePath,
  activity,
  onSelectCard,
  onOpenRoster,
}: Props) {
  const { lang } = usePrefs();
  const { agents } = useAgents();
  const { cards, reload: reloadCards } = useCards(workspaceId);
  const { goals, reload: reloadGoals } = useGoals();

  const lead = agents.find((a) => a.id === session?.agentId) ?? agents[0] ?? null;

  const plans = cards.filter((c) => {
    if (c.column === "done") return false;
    if (!session) return c.column === "running" || c.column === "review" || c.column === "ready";
    if (c.sessionId && c.sessionId === session.id) return true;
    if (c.assigneeAgentId && c.assigneeAgentId === session.agentId) return true;
    if (c.column === "running" || c.column === "review") return true;
    if (c.goalId && goals.some((g) => g.id === c.goalId && (g.agentId === session.agentId || g.workspaceId === session.workspaceId))) return true;
    return false;
  }).slice(0, 12);

  const sessionGoals = goals.filter((g) => {
    if (g.status === "done" || g.status === "failed") return false;
    if (!session) return g.status === "running" || g.status === "pending";
    return g.agentId === session.agentId || g.workspaceId === session.workspaceId || g.status === "running";
  }).slice(0, 8);

  const crew = agents.filter((a) => {
    if (!session) return a.status === "running" || Boolean(activity[a.id]);
    return a.id === session.agentId || a.status === "running" || Boolean(activity[a.id]);
  });
  const shown = crew.length ? crew : (lead ? [lead] : []);

  return (
    <div className="session-rail">
      {sessionGoals.map((g) => (
        <div key={g.id} className="nav-item" style={{ cursor: "default" }}>
          <span className={`pip${g.status === "running" ? " run" : ""}`} style={{ marginRight: 8 }} />
          <span style={{ display: "block" }}>{g.title}</span>
          <span style={{ display: "block", fontSize: 11, color: "var(--muted)", paddingLeft: 15 }}>
            {g.status} · iter {g.iteration}
          </span>
          {g.status !== "running" && (
            <button
              className="ghost"
              style={{ fontSize: 10, marginTop: 4 }}
              onClick={() => void rpc("goal.drive", {
                id: g.id,
                sessionId: session?.id ?? "",
                workspace: workspacePath ?? "",
              }).then(() => reloadGoals())}
            >
              ▶
            </button>
          )}
        </div>
      ))}
      {plans.map((c) => (
        <button
          key={c.id}
          className="nav-item"
          onClick={() => onSelectCard?.(c)}
        >
          <span className={`pip${c.column === "running" ? " run" : c.column === "review" ? " on" : ""}`} style={{ marginRight: 8 }} />
          <span style={{ display: "block" }}>{c.title}</span>
          <span style={{ display: "block", fontSize: 11, color: "var(--muted)", paddingLeft: 15 }}>
            {c.column}{c.assigneeAgentId ? ` · ${agents.find((a) => a.id === c.assigneeAgentId)?.name ?? ""}` : ""}
          </span>
        </button>
      ))}
      {sessionGoals.length === 0 && plans.length === 0 && (
        <div style={{ padding: "4px 14px", color: "var(--faint)", fontSize: 12 }}>{t("noneYet", lang)}</div>
      )}

      <div className="section-label section-label-row" style={{ marginTop: 8 }}>
        <span>{t("subagents", lang)}</span>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          {shown.filter((a) => a.status === "running" || Boolean(activity[a.id])).length > 1 && (
            <span className="agent-running-pill">
              {shown.filter((a) => a.status === "running" || Boolean(activity[a.id])).length} parallel
            </span>
          )}
          <button className="icon-btn" title={t("newProfile", lang)} onClick={onOpenRoster}>
            <Icon name="plus" size={15} />
          </button>
        </div>
      </div>
      {shown.length === 0 ? (
        <div style={{ padding: "4px 14px", color: "var(--faint)", fontSize: 12 }}>{t("noneYet", lang)}</div>
      ) : (
        shown.map((a) => {
          const doing = activity[a.id];
          const isLead = session?.agentId === a.id;
          const live = a.status === "running" || Boolean(doing);
          return (
            <div key={a.id} className="nav-item" style={{ cursor: "default" }}>
              <span className={`pip${live ? " run" : " on"}`} style={{ marginRight: 8 }} />
              <span style={{ display: "block" }}>
                {a.name}{isLead ? " · lead" : ""}
              </span>
              <span style={{ display: "block", fontSize: 11, color: "var(--muted)", paddingLeft: 15 }}>
                {doing || (live ? t("thinking", lang) : t("idle", lang))}
              </span>
              {live && (
                <button
                  className="ghost"
                  style={{ fontSize: 10, marginTop: 4 }}
                  onClick={() => void rpc("agent.cancel", { agentId: a.id }).then(() => reloadCards())}
                >
                  {t("stop", lang)}
                </button>
              )}
            </div>
          );
        })
      )}
    </div>
  );
}
