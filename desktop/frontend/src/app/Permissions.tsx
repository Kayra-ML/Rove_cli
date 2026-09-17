import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import { toast } from "~/lib/toast";
import { t } from "~/lib/i18n";
import { usePrefs } from "~/hooks/usePrefs";

type Rule = {
  id: string;
  action: string;
  pattern: string;
  decision: string;
  skill?: string;
  agentId?: string;
};

const ACTIONS = ["filesystem", "shell", "network", "browser", "git", "secrets"];
const DECISIONS = ["allow", "ask", "deny"];

export function Permissions() {
  const { lang } = usePrefs();
  const [rules, setRules] = useState<Rule[]>([]);
  const [draft, setDraft] = useState({ action: "shell", pattern: "*", decision: "ask" });

  const load = useCallback(async () => {
    try {
      setRules((await rpc<Rule[]>("permission.list")) ?? []);
    } catch {
      setRules([]);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const put = async (r: Rule) => {
    try {
      await rpc("permission.put", r);
      await load();
      toast(r.action, "ok");
    } catch (e) {
      toast(e instanceof Error ? e.message : "permission failed", "err");
    }
  };

  return (
    <>
      <h2>{t("permissions", lang)}</h2>
      <p className="split-lead">{t("permLead", lang)}</p>
      <div className="row-list">
        {rules.map((r) => (
          <div key={r.id} className="row">
            <div className="meta">
              <strong>{r.action}</strong>
              <span>{r.pattern || "*"}</span>
            </div>
            <select
              value={r.decision}
              onChange={(e) => void put({ ...r, decision: e.target.value })}
              style={{ fontSize: 12 }}
            >
              {DECISIONS.map((d) => <option key={d} value={d}>{d}</option>)}
            </select>
          </div>
        ))}
      </div>
      <div style={{ display: "flex", gap: 6, marginTop: 12, flexWrap: "wrap" }}>
        <select value={draft.action} onChange={(e) => setDraft({ ...draft, action: e.target.value })}>
          {ACTIONS.map((a) => <option key={a} value={a}>{a}</option>)}
        </select>
        <input
          placeholder="*"
          value={draft.pattern}
          onChange={(e) => setDraft({ ...draft, pattern: e.target.value })}
          style={{ flex: 1, minWidth: 80 }}
        />
        <select value={draft.decision} onChange={(e) => setDraft({ ...draft, decision: e.target.value })}>
          {DECISIONS.map((d) => <option key={d} value={d}>{d}</option>)}
        </select>
        <button
          className="primary"
          onClick={() => void put({
            id: `ui-${draft.action}-${Date.now()}`,
            action: draft.action,
            pattern: draft.pattern || "*",
            decision: draft.decision,
          })}
        >
          {t("save", lang)}
        </button>
      </div>
    </>
  );
}
