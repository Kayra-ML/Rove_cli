/**
 * ProfilePanel — Rol/Profil yönetimi
 * Her profil: isim, rol, sistem prompt (ne yapar / ne yapmaz), model, default, lider.
 * Roller arası iletişim: leader diğerlerine session.relay ile mesaj gönderir.
 */
import { useCallback, useState } from "react";
import { rpc } from "~/lib/rpc";
import type { AgentProfile, AgentRole } from "~/lib/types";
import { useProfiles } from "~/hooks/useApi";
import { Icon } from "./Icons";
import { usePrefs } from "~/hooks/usePrefs";

// ─── Rol tanımları ────────────────────────────────────────────────────────────
const ROLES: {
  value: AgentRole;
  label: string;
  emoji: string;
  desc: string;
  color: string;
  promptHint: string;       // "ne yapar" kısa açıklama
  promptTemplate: string;   // otomatik doldurulacak sistem prompt şablonu
}[] = [
  {
    value: "leader",
    label: "Lider",
    emoji: "👑",
    desc: "Diğer agent'ları koordine eder, görev dağıtır, çıktıları birleştirir",
    color: "#f59e0b",
    promptHint: "Görev alır, parçalar, uygun role yönlendirir, çıktıları toplar",
    promptTemplate:
      "Sen bir proje lideri agent'sın. Gelen görevi analiz et, uygun rollere (frontend, backend, tester, vb.) yönlendir. Her rolden gelen çıktıyı birleştir ve kullanıcıya temiz bir özet sun.\n\nYapacakların:\n- Görevi parçalara böl\n- Her parçayı en uygun role ata\n- Çıktıları birleştir ve özetle\n- Çakışmaları çöz\n\nYapmayacakların:\n- Bizzat kod yazmak\n- UI/UX kararı vermek\n- Test yazmak",
  },
  {
    value: "frontend",
    label: "Frontend",
    emoji: "🎨",
    desc: "React/TS bileşen geliştirme, UI mantığı, CSS, erişilebilirlik",
    color: "#06b6d4",
    promptHint: "Bileşen yazar, stil düzenler, UI state yönetir",
    promptTemplate:
      "Sen bir frontend geliştirici agent'sın. React, TypeScript ve CSS konularında uzmansın.\n\nYapacakların:\n- React bileşeni yaz, düzenle\n- TypeScript tip tanımları oluştur\n- CSS/Tailwind stilleri yaz\n- UI state yönet (useState, useReducer)\n- Erişilebilirlik (a11y) kontrolü yap\n- Responsive tasarım uygula\n\nYapmayacakların:\n- Veritabanı sorgusu yazmak\n- Sunucu tarafı iş mantığı\n- Altyapı/DevOps konfigürasyonu\n- Test yazımı (bunu tester yapar)",
  },
  {
    value: "backend",
    label: "Backend",
    emoji: "⚙️",
    desc: "API geliştirme, veritabanı, servis mantığı, güvenlik",
    color: "#3b82f6",
    promptHint: "API endpoint, DB sorgu, servis katmanı yazar",
    promptTemplate:
      "Sen bir backend geliştirici agent'sın. API tasarımı, veritabanı ve servis mimarisi konularında uzmansın.\n\nYapacakların:\n- REST/RPC API endpoint'leri yaz\n- Veritabanı şeması ve sorguları tasarla\n- İş mantığı (business logic) katmanı oluştur\n- Güvenlik kontrolü uygula (auth, validation)\n- Hata yönetimi ve loglama\n\nYapmayacakların:\n- UI bileşeni yazmak\n- CSS/stil düzenlemesi\n- Son kullanıcı akışı tasarlamak\n- Deployment/infra konfigürasyonu",
  },
  {
    value: "developer",
    label: "Geliştirici",
    emoji: "💻",
    desc: "Full-stack genel geliştirme, refactor, kod kalitesi",
    color: "#8b5cf6",
    promptHint: "Her türlü kodu yazar, refactor eder, bug düzeltir",
    promptTemplate:
      "Sen bir full-stack geliştirici agent'sın. Frontend ve backend her ikisinde de çalışabilirsin.\n\nYapacakların:\n- İstenen özelliği en temiz şekilde implement et\n- Kodu refactor et, tekrarları temizle\n- Bug düzelt\n- Mevcut kod stiline uy\n\nYapmayacakların:\n- Kapsamı genişletmek (istenenden fazlasını yapmak)\n- Test yazmadan PR açmak\n- Büyük mimari değişiklik önerisini direkt uygulamak",
  },
  {
    value: "designer",
    label: "Tasarımcı",
    emoji: "✏️",
    desc: "UI/UX kararları, tasarım sistemi, kullanıcı akışı, görsel bütünlük",
    color: "#ec4899",
    promptHint: "Komponent tasarlar, UX akışı belirler, tasarım sistemi korur",
    promptTemplate:
      "Sen bir UI/UX tasarımcı agent'sın. Kullanıcı deneyimi ve görsel bütünlük konularında uzmansın.\n\nYapacakların:\n- Bileşen görünüm ve hissini belirle\n- Kullanıcı akışını (user flow) tasarla\n- Tasarım sistemine uyumluluğu kontrol et (spacing, color, typography)\n- Erişilebilirlik rehberleri uygula\n- Prototip ve wireframe açıkla\n\nYapmayacakların:\n- Direkt kod yazmak\n- Veritabanı kararı vermek\n- Performans optimizasyonu\n- Backend mimarisi tasarlamak",
  },
  {
    value: "tester",
    label: "Test",
    emoji: "🧪",
    desc: "Test yazar, QA, regresyon, edge case analizi",
    color: "#10b981",
    promptHint: "Unit/E2E test yazar, test senaryosu üretir",
    promptTemplate:
      "Sen bir QA/Test mühendisi agent'sın. Yazılım kalitesi ve test coverage konularında uzmansın.\n\nYapacakların:\n- Unit test yaz (vitest, jest, go test)\n- Entegrasyon testi yaz\n- Edge case'leri belirle ve test et\n- Regresyon kontrol et\n- Test raporu oluştur\n\nYapmayacakların:\n- Üretim kodu yazmak (sadece test)\n- UI tasarımı\n- Deployment işlemleri\n- Kodu refactor etmek",
  },
  {
    value: "debugger",
    label: "Debug",
    emoji: "🔍",
    desc: "Hata analizi, root cause tespiti, log inceleme, fix önerisi",
    color: "#f97316",
    promptHint: "Hatayı bulur, kök nedeni tespit eder, fix önerir",
    promptTemplate:
      "Sen bir debug uzmanı agent'sın. Hata analizi ve root cause tespiti konularında uzmansın.\n\nYapacakların:\n- Stack trace ve log analizi yap\n- Kök nedeni (root cause) tespit et\n- Reproduksiyonu minimalize et\n- Fix önerisi sun, gerekçelendir\n- Yan etkilerini değerlendir\n\nYapmayacakların:\n- Fix'i direkt uygulamak (önce onay al)\n- Kapsam dışı refactor\n- Yeni özellik eklemek\n- Test yazmak (bunu tester yapar)",
  },
  {
    value: "reviewer",
    label: "Reviewer",
    emoji: "🔎",
    desc: "Kod review, PR inceleme, standart ve güvenlik kontrolü",
    color: "#a855f7",
    promptHint: "PR/kodu inceler, standartlara uyumu kontrol eder",
    promptTemplate:
      "Sen bir kod reviewer agent'sın. Kod kalitesi, güvenlik ve standartlara uyum konularında uzmansın.\n\nYapacakların:\n- Kodu satır satır incele\n- Güvenlik açıklarını tespit et\n- Performans sorunlarını belirle\n- Kod standartlarına uyumu kontrol et\n- Yapıcı geri bildirim yaz\n\nYapmayacakların:\n- Kodu direkt değiştirmek\n- Yeni özellik önermek\n- Mimari yeniden tasarım",
  },
  {
    value: "researcher",
    label: "Araştırmacı",
    emoji: "📚",
    desc: "Bilgi toplar, dokümante eder, analiz yapar, kaynak araştırır",
    color: "#14b8a6",
    promptHint: "Araştırır, özetler, dokümantasyon oluşturur",
    promptTemplate:
      "Sen bir araştırmacı agent'sın. Teknik araştırma ve dokümantasyon konularında uzmansın.\n\nYapacakların:\n- Teknik konuları araştır ve özetle\n- Kütüphane/araç karşılaştırması yap\n- Dokümantasyon yaz\n- API/SDK inceleme raporu hazırla\n- Best practice analizi sun\n\nYapmayacakların:\n- Kod yazmak\n- Mimari kararı vermek\n- Üretim değişikliği uygulamak",
  },
];

function roleColor(role: AgentRole) {
  return ROLES.find((r) => r.value === role)?.color ?? "var(--fg-muted)";
}
function roleLabel(role: AgentRole) {
  return ROLES.find((r) => r.value === role)?.label ?? role;
}
function roleEmoji(role: AgentRole) {
  return ROLES.find((r) => r.value === role)?.emoji ?? "";
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
  const [promptTab, setPromptTab] = useState<"custom" | "template">("custom");

  const openNew = () => {
    setEditing({ ...EMPTY });
    setPromptTab("custom");
  };
  const openEdit = (p: AgentProfile) => {
    setEditing({ ...p });
    setPromptTab("custom");
  };
  const close = () => setEditing(null);

  // Rol seçilince şablonu otomatik doldur (sadece prompt boşsa)
  const pickRole = (role: AgentRole) => {
    const tpl = ROLES.find((r) => r.value === role)?.promptTemplate ?? "";
    setEditing((prev) => {
      if (!prev) return prev;
      return {
        ...prev,
        role,
        isLeader: role === "leader",
        // eğer prompt henüz boş ya da şablon modundaysa, şablonu uygula
        systemPrompt: promptTab === "template" || !prev.systemPrompt?.trim() ? tpl : prev.systemPrompt,
      };
    });
  };

  const applyTemplate = () => {
    const tpl = ROLES.find((r) => r.value === editing?.role)?.promptTemplate ?? "";
    setEditing((prev) => prev ? { ...prev, systemPrompt: tpl } : prev);
    setPromptTab("custom");
  };

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

  const currentRole = ROLES.find((r) => r.value === editing?.role);

  return (
    <div className="profile-panel">
      <div className="profile-panel-head">
        <span className="profile-panel-title">Profiller</span>
        <button className="profile-add-btn" onClick={openNew}>
          <Icon name="plus" size={13} /> Yeni profil
        </button>
      </div>

      <div className="profile-default-info">
        <Icon name="flag" size={12} />
        <span>Default profil tüm yeni session'larda otomatik uygulanır. Lider profil diğer session'lara görev dağıtabilir.</span>
      </div>

      {profiles.length === 0 ? (
        <div className="empty" style={{ marginTop: 24 }}>
          <strong>Henüz profil yok</strong>
          <p>Profil oluşturun — her rol kendi sistem prompt şablonuyla gelir.</p>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((p) => (
            <div
              key={p.id}
              className={`profile-row${p.isDefault ? " profile-row-default" : ""}${p.isLeader ? " profile-row-leader" : ""}`}
            >
              <span className="profile-row-emoji">{roleEmoji(p.role)}</span>
              <div className="profile-row-info">
                <span className="profile-row-name">{p.name}</span>
                <span className="profile-row-meta">
                  <span className="profile-role-badge" style={{ color: roleColor(p.role) }}>
                    {roleLabel(p.role)}
                  </span>
                  {p.isLeader && <span className="profile-badge-leader">👑 Lider</span>}
                  {p.isDefault && <span className="profile-badge-default">● Default</span>}
                  {p.model && <span className="chip" style={{ fontSize: 10 }}>{p.model}</span>}
                  {p.systemPrompt && (
                    <span className="profile-badge-has-prompt" title={p.systemPrompt}>
                      prompt
                    </span>
                  )}
                </span>
              </div>
              <div className="profile-row-actions">
                {!p.isDefault && (
                  <button className="profile-btn" title="Default yap" onClick={() => setDefault(p.id)}>
                    <Icon name="flag" size={13} />
                  </button>
                )}
                <button className="profile-btn" title="Düzenle" onClick={() => openEdit(p)}>
                  <Icon name="sliders" size={13} />
                </button>
                <button className="profile-btn profile-btn-del" title="Sil" onClick={() => del(p.id)}>
                  <Icon name="trash" size={13} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* edit / create modal */}
      {editing && (
        <div className="profile-modal-backdrop" onClick={close}>
          <div className="profile-modal profile-modal-wide" onClick={(e) => e.stopPropagation()}>
            <div className="profile-modal-head">
              <span>{editing.id ? "Profili düzenle" : "Yeni profil"}</span>
              <button className="profile-btn" onClick={close}>✕</button>
            </div>

            {/* İsim */}
            <label>
              Profil adı
              <input
                className="profile-input"
                value={editing.name ?? ""}
                onChange={(e) => setEditing((prev) => prev ? { ...prev, name: e.target.value } : prev)}
                placeholder="örn. Frontend Uzmanı, API Dev, Test Bot..."
                autoFocus
              />
            </label>

            {/* Rol seçici */}
            <div>
              <div className="profile-label">Rol</div>
              <div className="profile-role-grid">
                {ROLES.map((r) => (
                  <button
                    key={r.value}
                    className={`profile-role-opt${editing.role === r.value ? " selected" : ""}`}
                    style={{ "--role-color": r.color } as React.CSSProperties}
                    onClick={() => pickRole(r.value)}
                    type="button"
                  >
                    <span className="profile-role-emoji">{r.emoji}</span>
                    <span>{r.label}</span>
                  </button>
                ))}
              </div>
              {currentRole && (
                <div className="profile-role-hint">
                  <span className="profile-role-hint-emoji">{currentRole.emoji}</span>
                  <span>{currentRole.promptHint}</span>
                </div>
              )}
            </div>

            {/* Sistem prompt */}
            <div>
              <div className="profile-prompt-head">
                <div className="profile-label">Sistem prompt</div>
                <div className="profile-prompt-tabs">
                  <button
                    className={`profile-prompt-tab${promptTab === "custom" ? " active" : ""}`}
                    onClick={() => setPromptTab("custom")}
                    type="button"
                  >
                    Özel
                  </button>
                  <button
                    className={`profile-prompt-tab${promptTab === "template" ? " active" : ""}`}
                    onClick={() => setPromptTab("template")}
                    type="button"
                  >
                    Şablon
                  </button>
                </div>
              </div>

              {promptTab === "template" ? (
                <div className="profile-template-preview">
                  <pre className="profile-template-text">{currentRole?.promptTemplate}</pre>
                  <button className="profile-template-apply" onClick={applyTemplate} type="button">
                    Bu şablonu kullan →
                  </button>
                </div>
              ) : (
                <textarea
                  className="profile-input profile-textarea"
                  value={editing.systemPrompt ?? ""}
                  onChange={(e) => setEditing((prev) => prev ? { ...prev, systemPrompt: e.target.value } : prev)}
                  placeholder={`Ne yapacağını ve ne yapmayacağını yaz...\n\nörn:\nYapacakların:\n- React bileşeni yaz\n- TypeScript tip tanımla\n\nYapmayacakların:\n- Backend kod\n- DB sorgusu`}
                  rows={8}
                />
              )}
            </div>

            {/* Model */}
            <label>
              Model <span className="profile-optional">(opsiyonel)</span>
              <input
                className="profile-input"
                value={editing.model ?? ""}
                onChange={(e) => setEditing((prev) => prev ? { ...prev, model: e.target.value } : prev)}
                placeholder="claude-opus-4, gpt-4o, grok-3..."
              />
            </label>

            {/* Checkboxlar */}
            <div className="profile-modal-checks">
              <label className="profile-check">
                <input
                  type="checkbox"
                  checked={editing.isDefault ?? false}
                  onChange={(e) => setEditing((prev) => prev ? { ...prev, isDefault: e.target.checked } : prev)}
                />
                <span>
                  <strong>Default profil</strong> — yeni tüm session'lara otomatik uygula
                </span>
              </label>
              <label className="profile-check">
                <input
                  type="checkbox"
                  checked={editing.isLeader ?? false}
                  onChange={(e) => setEditing((prev) => prev ? { ...prev, isLeader: e.target.checked } : prev)}
                />
                <span>
                  <strong>Lider</strong> — diğer session'lara görev relay'ler, çıktıları toplar
                </span>
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