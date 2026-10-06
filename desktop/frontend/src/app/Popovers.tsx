import { useCallback, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { Agent, Provider, SSHTarget } from "~/lib/types";
import { useAgents, useProviders } from "~/hooks/useApi";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

const SSH_HOSTS_KEY = "aether.sshHosts";

export function loadSSHHosts(): SSHTarget[] {
  try {
    const raw = localStorage.getItem(SSH_HOSTS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as SSHTarget[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function hostKey(h: SSHTarget): string {
  return `${h.user ?? ""}@${h.host}:${h.port ?? 22}`;
}

// saveSSHHost upserts by host+user+port, most recent first. Settings' Sunucular
// section owns this list; connecting from there is what actually opens a shell.
export function saveSSHHost(target: SSHTarget): SSHTarget[] {
  const next = loadSSHHosts().filter((h) => hostKey(h) !== hostKey(target));
  next.unshift(target);
  const trimmed = next.slice(0, 24);
  localStorage.setItem(SSH_HOSTS_KEY, JSON.stringify(trimmed));
  return trimmed;
}

export function removeSSHHost(target: SSHTarget): SSHTarget[] {
  const next = loadSSHHosts().filter((h) => hostKey(h) !== hostKey(target));
  localStorage.setItem(SSH_HOSTS_KEY, JSON.stringify(next));
  return next;
}

export function AgentRoster() {
  const { lang } = usePrefs();
  const { agents, reload } = useAgents();
  const { providers } = useProviders();
  const [form, setForm] = useState<Partial<Agent>>({ name: "", provider: "", model: "", profile: "default", systemPrompt: "" });
  const [editing, setEditing] = useState<Agent | null>(null);
  const [creating, setCreating] = useState(false);
  const [err, setErr] = useState("");
  const models = providers.find((p: Provider) => p.name === form.provider)?.models ?? [];
  const showForm = creating || editing !== null;

  const save = useCallback(async () => {
    if (!form.name || !form.provider || !form.model) {
      setErr("Name, provider and model are required.");
      return;
    }
    await rpc("agent.upsert", { ...editing, ...form });
    setForm({ name: "", provider: "", model: "", profile: "default", systemPrompt: "" });
    setEditing(null);
    setCreating(false);
    await reload();
  }, [form, editing, reload]);

  return (
    <>
      <h2>{t("roster", lang)}</h2>
      <p className="split-lead">{t("rosterLead", lang)}</p>
      {!showForm && (
        <>
          {agents.length === 0 ? (
            <div className="empty">
              <strong>No agents yet</strong>
              <p>Create one with a provider and model.</p>
              <button className="primary" onClick={() => setCreating(true)}>{t("newAgent", lang)}</button>
            </div>
          ) : (
            <div className="row-list" style={{ marginBottom: 14 }}>
              {agents.map((a: Agent) => (
                <div key={a.id} className="row" onClick={() => { setEditing(a); setCreating(false); setForm({ name: a.name, provider: a.provider, model: a.model, profile: a.profile, systemPrompt: a.systemPrompt }); }} style={{ cursor: "pointer" }}>
                  <div className="meta">
                    <strong>{a.name}</strong>
                    <span>{a.provider} / {a.model}</span>
                  </div>
                  <span className={`pip${a.status === "running" ? " run" : " on"}`} />
                </div>
              ))}
            </div>
          )}
          {agents.length > 0 && <button className="primary" onClick={() => { setCreating(true); setEditing(null); }}>{t("newAgent", lang)}</button>}
        </>
      )}
      {showForm && (
        <div className="form-stack">
          <label>Name</label>
          <input value={form.name ?? ""} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          <label>Provider</label>
          <select value={form.provider ?? ""} onChange={(e) => setForm((f) => ({ ...f, provider: e.target.value, model: "" }))}>
            <option value="">Select…</option>
            {providers.map((p: Provider) => <option key={p.id} value={p.name}>{p.name}</option>)}
          </select>
          <label>Model</label>
          {models.length > 0 ? (
            <select value={form.model ?? ""} onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}>
              <option value="">Select…</option>
              {models.map((m) => <option key={m} value={m}>{m}</option>)}
            </select>
          ) : (
            <input value={form.model ?? ""} onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))} />
          )}
          <label>System prompt</label>
          <textarea rows={4} value={form.systemPrompt ?? ""} onChange={(e) => setForm((f) => ({ ...f, systemPrompt: e.target.value }))} />
          {err && <div className="danger">{err}</div>}
          <div style={{ display: "flex", gap: 8 }}>
            <button className="primary" onClick={() => void save()}>{editing ? "Save" : "Create"}</button>
            {editing && (
              <button className="danger-btn" onClick={() => void rpc("agent.delete", { id: editing.id }).then(() => { setEditing(null); setCreating(false); void reload(); })}>Delete</button>
            )}
            <button className="ghost" onClick={() => { setCreating(false); setEditing(null); }}>Cancel</button>
          </div>
        </div>
      )}
    </>
  );
}
