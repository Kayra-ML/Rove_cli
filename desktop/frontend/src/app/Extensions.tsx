import { useSkills } from "~/hooks/useApi";
import type { InstalledSkill } from "~/lib/types";
import { Icon } from "./Icons";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";

interface Props {
  onOpenMarket: () => void;
  marketActive?: boolean;
}

export function Extensions({ onOpenMarket, marketActive }: Props) {
  const { skills } = useSkills();
  const { lang } = usePrefs();

  return (
    <div>
      <div className="section-label">{t("extensions", lang)}</div>
      <button
        className={`nav-item${marketActive ? " active" : ""}`}
        onClick={onOpenMarket}
      >
        <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
          <Icon name="cart" size={16} />
          {t("marketplace", lang)}
        </span>
      </button>
      {skills.length === 0 ? (
        <div style={{ color: "var(--faint)", padding: "4px 12px 8px", fontSize: 11 }}>
          {t("noneYet", lang)}
        </div>
      ) : (
        skills.map((s: InstalledSkill) => (
          <div
            key={s.manifest.name}
            className="nav-item"
            title={s.manifest.description}
            style={{ cursor: "default" }}
          >
            <span style={{ display: "block", fontSize: 13.5 }}>{s.manifest.name}</span>
            <span style={{ display: "block", fontSize: 11, color: "var(--muted)" }}>
              v{s.manifest.version}{s.enabled === false ? ` · ${t("disabled", lang)}` : ""}
            </span>
          </div>
        ))
      )}
    </div>
  );
}
