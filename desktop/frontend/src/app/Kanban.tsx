import { useCallback, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { Agent, Card, Column } from "~/lib/types";
import { useAgents, useCards } from "~/hooks/useApi";
import { toast } from "~/lib/toast";
import { t } from "~/lib/i18n";
import { usePrefs } from "~/hooks/usePrefs";

const COL_KEY = "aether.kanban.collapsed";

const COLUMNS: { key: Column; label: string }[] = [
  { key: "backlog", label: "Backlog" },
  { key: "ready", label: "Ready" },
  { key: "running", label: "Running" },
  { key: "review", label: "Review" },
  { key: "done", label: "Done" },
  { key: "blocked", label: "Blocked" },
];

function loadCollapsed(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(COL_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, boolean>;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

interface Props {
  workspaceId?: string;
  sessionId?: string;
  onSelectCard?: (card: Card) => void;
}

export function Kanban({ workspaceId, sessionId, onSelectCard }: Props) {
  const { cards, reload } = useCards(workspaceId);
  const { agents } = useAgents();
  const { lang } = usePrefs();
  const [draft, setDraft] = useState<{ col: Column; title: string } | null>(null);
  const [over, setOver] = useState<Column | null>(null);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(loadCollapsed);

  const toggleCol = (key: Column) => {
    setCollapsed((prev) => {
      const next = { ...prev, [key]: !prev[key] };
      localStorage.setItem(COL_KEY, JSON.stringify(next));
      return next;
    });
  };

  const move = useCallback(
    async (cardId: string, col: Column) => {
      try {
        await rpc("card.move", { id: cardId, column: col });
        await reload();
      } catch (e) {
        toast(e instanceof Error ? e.message : "move failed", "err");
      }
    },
    [reload],
  );

  const dispatch = useCallback(
    async (cardId: string) => {
      try {
        await rpc("card.dispatch", { cardId, sessionId: sessionId ?? "" });
        await reload();
        toast("running", "ok");
      } catch (e) {
        toast(e instanceof Error ? e.message : "dispatch failed", "err");
      }
    },
    [reload, sessionId],
  );

  const removeCard = useCallback(
    async (cardId: string) => {
      try {
        await rpc("card.delete", { id: cardId });
        await reload();
      } catch (e) {
        toast(e instanceof Error ? e.message : "delete failed", "err");
      }
    },
    [reload],
  );

  const assign = useCallback(
    async (cardId: string, agentId: string) => {
      try {
        await rpc("card.assign", { id: cardId, agentId });
        await reload();
      } catch (e) {
        toast(e instanceof Error ? e.message : "assign failed", "err");
      }
    },
    [reload],
  );

  const createCard = useCallback(
    async (col: Column, title: string) => {
      const name = title.trim();
      if (!name) return;
      try {
        await rpc("card.create", {
          title: name,
          column: col,
          workspaceId: workspaceId ?? "",
          sessionId: sessionId ?? "",
        });
        setDraft(null);
        await reload();
        toast(name, "ok");
      } catch (e) {
        toast(e instanceof Error ? e.message : "create failed", "err");
      }
    },
    [reload, workspaceId, sessionId],
  );

  return (
    <div className="kanban">
      {cards.filter((c: Card) => c.column === "running").length > 1 && (
        <div className="parallel-banner" style={{ position: "absolute", top: 0, left: 10, right: 10, zIndex: 10 }}>
          ⚡ {cards.filter((c: Card) => c.column === "running").length} agents running in parallel
        </div>
      )}
      {COLUMNS.map(({ key, label }) => {
        const colCards = cards.filter((c: Card) => c.column === key);
        const drop = over === key;
        const shut = Boolean(collapsed[key]);
        const drag = {
          onDragOver: (e: React.DragEvent) => { e.preventDefault(); setOver(key); },
          onDragLeave: () => setOver((cur) => (cur === key ? null : cur)),
          onDrop: (e: React.DragEvent) => {
            e.preventDefault();
            setOver(null);
            const id = e.dataTransfer.getData("text/aether-card");
            if (id) void move(id, key);
          },
        };
        if (shut) {
          return (
            <button
              key={key}
              type="button"
              className={`column spine${drop ? " drop" : ""}`}
              {...drag}
              onClick={() => toggleCol(key)}
              title={label}
            >
              <span className="col-dot" style={{ background: `var(--col-${key})`, color: `var(--col-${key})` }} />
              <span className="col-spine-label" style={{ color: `var(--col-${key})` }}>
                {label}
              </span>
              {colCards.length > 0 && <span className="col-spine-n">{colCards.length}</span>}
            </button>
          );
        }
        return (
          <div
            key={key}
            className={`column${drop ? " drop" : ""}`}
            {...drag}
          >
            <h3>
              <span className="col-dot" style={{ background: `var(--col-${key})`, color: `var(--col-${key})` }} />
              <span className="col-title" style={{ color: `var(--col-${key})` }}>{label}</span>
              <span className="col-n">{colCards.length}</span>
              <button
                type="button"
                className="col-add"
                onClick={() => setDraft({ col: key, title: "" })}
              >
                +
              </button>
              <button
                type="button"
                className="col-fold"
                onClick={() => toggleCol(key)}
                title={label}
              >
                ‹
              </button>
            </h3>
            <div className="cards">
              {colCards.map((c: Card) => (
                <CardTile
                  key={c.id}
                  card={c}
                  agents={agents}
                  onMove={move}
                  onDispatch={dispatch}
                  onSelect={onSelectCard}
                  onDelete={removeCard}
                  onAssign={assign}
                />
              ))}
              {draft?.col === key ? (
                <input
                  autoFocus
                  className="col-draft"
                  value={draft.title}
                  placeholder={t("create", lang)}
                  onChange={(e) => setDraft({ col: key, title: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void createCard(key, draft.title);
                    if (e.key === "Escape") setDraft(null);
                  }}
                  onBlur={() => {
                    if (draft.title.trim()) void createCard(key, draft.title);
                    else setDraft(null);
                  }}
                />
              ) : (
                <button
                  type="button"
                  className="col-new"
                  onClick={() => setDraft({ col: key, title: "" })}
                >
                  + {t("create", lang)}
                </button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function CardTile({
  card,
  agents,
  onMove,
  onDispatch,
  onSelect,
  onDelete,
  onAssign,
}: {
  card: Card;
  agents: Agent[];
  onMove: (id: string, col: Column) => void;
  onDispatch: (id: string) => void;
  onSelect?: (c: Card) => void;
  onDelete: (id: string) => void;
  onAssign: (id: string, agentId: string) => void;
}) {
  const isRunning = card.column === "running";
  return (
    <div
      className={`card${isRunning ? " card-running-overlay" : ""}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("text/aether-card", card.id);
        e.dataTransfer.effectAllowed = "move";
      }}
      onClick={() => onSelect?.(card)}
    >
      <h4>{card.title}</h4>
      {card.description && (
        <p style={{ fontSize: 11, marginBottom: 6 }}>
          {card.description.slice(0, 80)}
        </p>
      )}
      <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginTop: 4 }}>
        {card.goalMode && <span className="badge">goal</span>}
        {card.gitBranch && (
          <span className="badge" style={{ fontSize: 9 }}>
            {card.gitBranch.slice(0, 18)}
          </span>
        )}
      </div>
      <div className="card-actions" onClick={(e) => e.stopPropagation()}>
        {card.column === "ready" && (
          <button
            className="run-btn"
            onClick={() => onDispatch(card.id)}
          >
            ▶
          </button>
        )}
        {card.column === "review" && (
          <>
            <button
              className="ok-btn"
              onClick={() => {
                void rpc("card.review", { id: card.id, state: "approved" }).then(() => onMove(card.id, "done"));
              }}
            >
              ✓
            </button>
            <button
              className="bad-btn"
              onClick={() => {
                void rpc("card.review", { id: card.id, state: "rejected" }).then(() => onMove(card.id, "blocked"));
              }}
            >
              ✗
            </button>
          </>
        )}
        {agents.length > 0 && (
          <select
            className="assign-sel"
            value={card.assigneeAgentId ?? ""}
            onChange={(e) => onAssign(card.id, e.target.value)}
          >
            <option value="">—</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>{a.name}</option>
            ))}
          </select>
        )}
        <button className="ghost card-x" onClick={() => onDelete(card.id)}>×</button>
      </div>
    </div>
  );
}
