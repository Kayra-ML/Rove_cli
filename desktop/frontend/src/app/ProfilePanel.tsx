/**
 * ProfilePanel — Rol/Profil yönetimi
 * Profiller default olarak işaretlenirse tüm yeni session'larda otomatik uygulanır.
 * Lider profil diğer session'lara relay mesaj gönderir ve koordine eder.
 */
import { useCallback, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { AgentProfile, AgentRole } from "~/lib/types";
import { useProfiles } from "~/hooks/useApi";
import { Icon } from "./Icons";
import { usePrefs } from "~/hooks/usePrefs";

const ROLES: { value: AgentRole; label: string; desc: string; color: string }[] = [
  { value: "leader",     label: "Lider",      desc: "Diğer agent'ları koordine eder, çıktıları birleştirir",  color: "#f59e0b" },
  { value: "developer",  label: "Geliştirici", desc: "Kod yazar, refactor eder, bug düzeltir",                color: "#3b82f6" },
  { value: "reviewer",   label: "Reviewer",    desc: "Kod ve PR review yapar, kalite kontrol",                 color: "#8b5cf6" },
  { value: "researcher", label: "Araştırmacı", desc: "Bilgi toplar, dokümante eder, analiz yapar",            color: "#06b6d4" },
  { value: "tester",     label: "Test",        desc: "Test yazar, QA, regresyon kontrol",                     color: "#10b981" },
  { value: "designer",   label: "Tasarımcı",   desc: "UI/UX kararları, component tasarımı",                   color: "#ec4899" },
];

function roleColor(role: AgentRole) {
  return ROLES.find((r) => r.value === role)?.color ?? "var(--fg-muted)";
}

function roleLabel(role: AgentRole) {
  return ROLES.find((r) => r.value === role)?.label ?? role;
}

const EMPTY: Omit<AgentProfile, "id" | "createdAt" | "updatedAt"> = {
  name: "",
  role: "developer",
  systemPrompt: "",
  model: "",
  provider: "",
  isDefault: false,
  isLeader: false,
  color: "",
};

export function ProfilePanel() {
  const { profiles, reload } = useProfiles();
  const { lang: _lang } = usePrefs();
  const [editing, setEditing] = useState<Partial<AgentProfile> | null>(null);
  const [busy, setBusy] = useState(false);

  const openNew = () => setEditing({ ...EMPTY });
  const openEdit = (p: AgentProfile) => setEditing({ ...p });
  const close = () => setEditing(null);

  const save = useCallback(async () => {
    if (!editing?.name?.trim()) return;
    setBusy(true);
    try {
      await rpc("profile.upsert", editing);
      await reload();
      close();
    } finally {
      setBusy(false);
    }
  }, [editing, reload]);

  const del = useCallback(async (id: string) => {
    if (!confirm("Bu profili sil?")) return;
    await rpc("profile.delete", { id });
    await reload();
  }, [reload]);

  const setDefault = useCallback(async (id: string) => {
    await rpc("profile.setDefault", { id });
    await reload();
  }, [reload]);

  return (
    <div className="profile-panel">
      <div className="profile-panel-head">
        <span className="profile-panel-title">Profiller</span>
        <button className="profile-add-btn" onClick={openNew}>
          <Icon name="plus" size={13} /> Yeni profil
        </button>
      </div>

      {/* default info */}
      <div className="profile-default-info">
        <Icon name="flag" size={12} />
        <span>Default profil tüm yeni session'larda otomatik uygulanır.</span>
      </div>

      {/* profile list */}
      {profiles.length === 0 ? (
        <div className="empty" style={{ marginTop: 24 }}>
          <strong>Henüz profil yok</strong>
          <p>Profil oluşturun; rollere göre agent davranışını özelleştirin.</p>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((p) => (
            <div key={p.id} className={`profile-row${p.isDefault ? " profile-row-default" : ""}${p.isLeader ? " profile-row-leader" : ""}`}>
              <div className="profile-row-role-dot" style={{ background: roleColor(p.role) }} />
              <div className="profile-row-info">
                <span className="profile-row-name">{p.name}</span>
                <span className="profile-row-meta">
                  <span className="profile-role-badge" style={{ color: roleColor(p.role) }}>
                    {roleLabel(p.role)}
                  </span>
                  {p.isLeader && <span className="profile-badge-leader">👑 Lider</span>}
                  {p.isDefault && <span className="profile-badge-default">● Default</span>}
                  {p.model && <span className="chip" style={{ fontSize: 10 }}>{p.model}</span>}
                </span>
              </div>
              <div className="profile-row-actions">
                {!p.isDefault && (
                  <button
                    className="profile-btn"
                    title="Default yap"
                    onClick={() => setDefault(p.id)}
                  >
                    <Icon name="flag" size={13} />
                  </button>
                )}
                <button className="profile-btn" title="Düzenle" onClick={() => openEdit(p)}>
                  <Icon name="sliders" size={13} />
                </button>
                <button
                  className="profile-btn profile-btn-del"
                  title="Sil"
                  onClick={() => del(p.id)}
                >
                  <Icon name="trash" size={13} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* edit/create modal */}
      {editing && (
        <div className="profile-modal-backdrop" onClick={close}>
          <div className="profile-modal" onClick={(e) => e.stopPropagation()}>
            <div className="profile-modal-head">
              <span>{editing.id ? "Profili düzenle" : "Yeni profil"}</span>
              <button className="profile-btn" onClick={close}>✕</button>
            </div>

            <label>
              İsim
              <input
                className="profile-input"
                value={editing.name ?? ""}
                onChange={(e) => setEditing((prev) => prev ? { ...prev, name: e.target.value } : prev)}
                placeholder="Profil adı"
              />
            </label>

            <label>
              Rol
              <div className="profile-role-grid">
                {ROLES.map((r) => (
                  <button
                    key={r.value}
                    className={`profile-role-opt${editing.role === r.value ? " selected" : ""}`}
                    style={{ "--role-color": r.color } as React.CSSProperties}
                    onClick={() => setEditing((prev) => prev ? { ...prev, role: r.value, isLeader: r.value === "leader" } : prev)}
                  >
                    <span className="profile-role-dot" style={{ background: r.color }} />
                    <span>{r.label}</span>
                  </button>
                ))}
              </div>
              <span className="profile-role-desc">{ROLES.find((r) => r.value === editing.role)?.desc}</span>
            </label>

            <label>
              Sistem prompt (opsiyonel)
              <textarea
                className="profile-input profile-textarea"
                value={editing.systemPrompt ?? ""}
                onChange={(e) => setEditing((prev) => prev ? { ...prev, systemPrompt: e.target.value } : prev)}
                placeholder="Bu profil için özel sistem prompt..."
                rows={3}
              />
            </label>

            <label>
              Model (opsiyonel)
              <input
                className="profile-input"
                value={editing.model ?? ""}
                onChange={(e) => setEditing((prev) => prev ? { ...prev, model: e.target.value } : prev)}
                placeholder="claude-opus-4, gpt-4o..."
              />
            </label>

            <div className="profile-modal-checks">
              <label className="profile-check">
                <input
                  type="checkbox"
                  checked={editing.isDefault ?? false}
                  onChange={(e) => setEditing((prev) => prev ? { ...prev, isDefault: e.target.checked } : prev)}
                />
                <span>Default profil — yeni tüm session'lara otomatik uygula</span>
              </label>
              <label className="profile-check">
                <input
                  type="checkbox"
                  checked={editing.isLeader ?? false}
                  onChange={(e) => setEditing((prev) => prev ? { ...prev, isLeader: e.target.checked } : prev)}
                />
                <span>Lider — diğer session'ları koordine eder</span>
              </label>
            </div>

            <div className="profile-modal-foot">
              <button className="profile-btn" onClick={close}>İptal</button>
              <button
                className="autom-install"
                disabled={busy || !editing.name?.trim()}
                onClick={save}
              >
                {busy ? "…" : editing.id ? "Kaydet" : "Oluştur"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}