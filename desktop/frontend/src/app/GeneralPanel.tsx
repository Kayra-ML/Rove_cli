import { useCallback, useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { Agent } from "~/lib/types";
import { useAgents } from "~/hooks/useApi";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

const KEY = "aether.globalSystemPrompt";

export function GeneralPanel() {
  const { agents, reload } = useAgents();
  const { lang } = usePrefs();
  const [prompt, setPrompt] = useState("");
  const [saved, setSaved] = useState(false);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const local = localStorage.getItem(KEY);
    if (local != null) {
      setPrompt(local);
      return;
    }
    const fromAgent = agents.find((a) => a.systemPrompt)?.systemPrompt ?? "";
    if (fromAgent) setPrompt(fromAgent);
  }, [agents]);

  const save = useCallback(async () => {
    localStorage.setItem(KEY, prompt);
    const target: Agent | undefined = agents[0];
    if (target) {
      await rpc("agent.upsert", { ...target, systemPrompt: prompt });
      await reload();
    }
    setSaved(true);
    window.setTimeout(() => setSaved(false), 1400);
  }, [prompt, agents, reload]);

  return (
    <div className="sidebar-bottom">
      <button className="section-label" style={{ width: "100%", textAlign: "left", background: "none", border: 0, padding: "12px 12px 6px" }} onClick={() => setOpen((o) => !o)}>
        {t("general", lang)} {open ? "▾" : "▸"}
      </button>
      {open && (
        <div style={{ padding: "0 10px 12px" }}>
          <label style={{ display: "block", fontSize: 10, color: "var(--faint)", letterSpacing: "0.08em", textTransform: "uppercase", marginBottom: 6 }}>
            {t("systemPrompt", lang)}
          </label>
          <textarea
            className="general-prompt"
            rows={6}
            placeholder="Tüm agent’lere gidecek genel talimat…"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
          <button className="primary" style={{ width: "100%", marginTop: 8, fontSize: 12 }} onClick={() => void save()}>
            {saved ? "✓" : t("save", lang)}
          </button>
        </div>
      )}
    </div>
  );
}
