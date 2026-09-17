import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import { toast } from "~/lib/toast";
import { t } from "~/lib/i18n";
import { usePrefs } from "~/hooks/usePrefs";

type Scope = "global" | "workspace" | "session" | "agent";

type Entry = {
  id: string;
  scope: Scope;
  scopeId?: string;
  key: string;
  content: string;
  createdAt?: string;
};

const SCOPES: Scope[] = ["global", "workspace", "session", "agent"];

interface Props {
  workspaceId?: string;
  sessionId?: string;
}

export function Memory({ workspaceId, sessionId }: Props) {
  const { lang } = usePrefs();
  const [scope, setScope] = useState<Scope>("global");
  const [items, setItems] = useState<Entry[]>([]);
  const [key, setKey] = useState("");
  const [content, setContent] = useState("");

  const scopeId = scope === "workspace" ? (workspaceId ?? "") : scope === "session" ? (sessionId ?? "") : "";

  const load = useCallback(async () => {
    try {
      const rows = await rpc<Entry[]>("memory.list", { scope, scopeId });
      setItems(rows ?? []);
    } catch (err) {
      setItems([]);
      toast(err instanceof Error ? err.message : "memory.list", "err");
    }
  }, [scope, scopeId]);

  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (!key.trim() || !content.trim()) return;
    try {
      await rpc("memory.put", { scope, scopeId, key: key.trim(), content: content.trim() });
      setKey("");
      setContent("");
      toast(t("save", lang), "ok");
      await load();
    } catch (err) {
      toast(err instanceof Error ? err.message : "memory.put", "err");
    }
  };

  return (
    <>
      <h2>{t("memory", lang)}</h2>
      <p className="split-lead">{t("memoryLead", lang)}</p>
      <div className="chip-row" style={{ marginBottom: 12 }}>
        {SCOPES.map((s) => (
          <button
            key={s}
            className={`chip${scope === s ? " active" : ""}`}
            onClick={() => setScope(s)}
          >
            {s}
          </button>
        ))}
      </div>
      <div style={{ display: "flex", gap: 6, marginBottom: 10 }}>
        <input
          style={{ width: 140 }}
          placeholder="key"
          value={key}
          onChange={(e) => setKey(e.target.value)}
        />
        <input
          style={{ flex: 1 }}
          placeholder="content"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") void save(); }}
        />
        <button onClick={() => void save()}>{t("save", lang)}</button>
      </div>
      {items.length === 0 ? (
        <div className="empty">
          <strong>{t("noneYet", lang)}</strong>
        </div>
      ) : (
        <div className="row-list">
          {items.map((e) => (
            <div key={e.id || e.key} className="row">
              <div className="meta">
                <strong>{e.key}</strong>
                <span>{e.content}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  );
}
