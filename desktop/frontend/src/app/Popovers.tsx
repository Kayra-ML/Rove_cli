import { useCallback, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { Agent, Provider, SSHTarget, TerminalSession } from "~/lib/types";
import { useAgents, useProviders } from "~/hooks/useApi";
import { Icon } from "./Icons";

export function CreateAgentPopover({ onClose }: { onClose: () => void }) {
  const { reload } = useAgents();
  const { providers } = useProviders();
  const [form, setForm] = useState({ name: "", provider: "", model: "", profile: "default", systemPrompt: "" });
  const [err, setErr] = useState("");
  const models = providers.find((p: Provider) => p.name === form.provider)?.models ?? [];

  const save = useCallback(async () => {
    if (!form.name || !form.provider || !form.model) {
      setErr("Name, provider, model zorunlu.");
      return;
    }
    await rpc("agent.upsert", form);
    await reload();
    onClose();
  }, [form, reload, onClose]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="popover-sheet" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h1>Yeni profil</h1>
          <button className="icon-btn" onClick={onClose}>✕</button>
        </header>
        <div className="form-stack" style={{ padding: 20, maxWidth: "none" }}>
          <label>Name</label>
          <input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="Claude" />
          <label>Provider</label>
          <select value={form.provider} onChange={(e) => setForm((f) => ({ ...f, provider: e.target.value, model: "" }))}>
            <option value="">Select…</option>
            {providers.map((p: Provider) => <option key={p.id} value={p.name}>{p.name}</option>)}
          </select>
          <label>Model</label>
          {models.length > 0 ? (
            <select value={form.model} onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}>
              <option value="">Select…</option>
              {models.map((m) => <option key={m} value={m}>{m}</option>)}
            </select>
          ) : (
            <input value={form.model} placeholder="gpt-4o" onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))} />
          )}
          <label>System prompt</label>
          <textarea rows={4} value={form.systemPrompt} onChange={(e) => setForm((f) => ({ ...f, systemPrompt: e.target.value }))} />
          {err && <div className="danger">{err}</div>}
          <button className="primary" onClick={() => void save()}>Create</button>
        </div>
      </div>
    </div>
  );
}

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

function saveSSHHost(t: SSHTarget) {
  const next = loadSSHHosts().filter((h) => !(h.host === t.host && (h.user ?? "") === (t.user ?? "") && (h.port ?? 22) === (t.port ?? 22)));
  next.unshift(t);
  localStorage.setItem(SSH_HOSTS_KEY, JSON.stringify(next.slice(0, 24)));
}

export function ConnectSSHPopover({
  onClose,
  onOpened,
}: {
  onClose: () => void;
  onOpened: (sess: TerminalSession) => void;
}) {
  const [form, setForm] = useState({ host: "", port: "22", user: "", authMethod: "agent", keyPath: "" });
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const save = useCallback(async () => {
    const host = form.host.trim();
    if (!host) {
      setErr("host zorunlu");
      return;
    }
    setBusy(true);
    setErr("");
    const target: SSHTarget = {
      host,
      port: Number(form.port) || 22,
      user: form.user.trim() || undefined,
      authMethod: form.authMethod,
      keyPath: form.keyPath.trim() || undefined,
    };
    try {
      const sess = await rpc<TerminalSession>("ssh.open", target);
      saveSSHHost(target);
      onOpened(sess);
      onClose();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "ssh failed");
    } finally {
      setBusy(false);
    }
  }, [form, onClose, onOpened]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="popover-sheet" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h1>SSH bağla</h1>
          <button className="icon-btn" onClick={onClose}>✕</button>
        </header>
        <div className="form-stack" style={{ padding: 20, maxWidth: "none" }}>
          <label>Host</label>
          <input value={form.host} placeholder="217.217.233.132" autoFocus onChange={(e) => setForm((f) => ({ ...f, host: e.target.value }))} />
          <label>Port</label>
          <input value={form.port} placeholder="22" onChange={(e) => setForm((f) => ({ ...f, port: e.target.value }))} />
          <label>User</label>
          <input value={form.user} placeholder="burak" onChange={(e) => setForm((f) => ({ ...f, user: e.target.value }))} />
          <label>Auth</label>
          <select value={form.authMethod} onChange={(e) => setForm((f) => ({ ...f, authMethod: e.target.value }))}>
            <option value="agent">ssh-agent</option>
            <option value="key">private key</option>
            <option value="password">password (prompt in pty)</option>
          </select>
          {form.authMethod === "key" && (
            <>
              <label>Key path</label>
              <input value={form.keyPath} placeholder="~/.ssh/id_ed25519" onChange={(e) => setForm((f) => ({ ...f, keyPath: e.target.value }))} />
            </>
          )}
          {err && <div className="danger">{err}</div>}
          <button className="primary" disabled={busy} onClick={() => void save()}>{busy ? "…" : "Connect"}</button>
        </div>
      </div>
    </div>
  );
}

export function AgentRoster() {
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
      <h2>Roster</h2>
      <p className="split-lead">Named model + prompt. Orchestrator buradan dispatch eder.</p>
      {!showForm && (
        <>
          {agents.length === 0 ? (
            <div className="empty">
              <strong>No agents yet</strong>
              <p>Create one with a provider and model.</p>
              <button className="primary" onClick={() => setCreating(true)}>New agent</button>
            </div>
          ) : (
            <div className="row-list" style={{ marginBottom: 14 }}>
              {agents.map((a: Agent) => (
                <div key={a.id} className="row" onClick={() => { setEditing(a); setCreating(false); setForm({ name: a.name, provider: a.provider, model: a.model, profile: a.profile, systemPrompt: a.systemPrompt }); }} style={{ cursor: "pointer" }}>
                  <div className="avatar">{a.name.slice(0, 1).toUpperCase()}</div>
                  <div className="meta">
                    <strong>{a.name}</strong>
                    <span>{a.provider} / {a.model}</span>
                  </div>
                  <span className={`pip${a.status === "running" ? " run" : " on"}`} />
                </div>
              ))}
            </div>
          )}
          {agents.length > 0 && <button className="primary" onClick={() => { setCreating(true); setEditing(null); }}>New agent</button>}
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
