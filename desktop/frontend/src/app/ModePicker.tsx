import { useEffect, useRef } from "react";
import { t, type Lang } from "~/lib/i18n";
import { CHAT_MODES, type ChatMode } from "~/lib/spaces";
import type { Workspace } from "~/lib/types";
import { Icon } from "./Icons";

// The icon each mode goes by, wherever a mode is named.
export const MODE_ICON: Record<ChatMode, "orchestra" | "teamwork" | "terminal"> = {
  orchestra: "orchestra",
  teamwork: "teamwork",
  terminal: "terminal",
};

type Props = {
  lang: Lang;
  workspace: Workspace | null;
  // the mode used most recently, marked so the usual pick is one glance away
  last?: ChatMode;
  onPick: (mode: ChatMode) => void;
};

// ModePicker is where a session starts: the mode it will run in is chosen
// here, once. A conversation, a plan and a terminal deck keep different
// state, so the mode stays with the session for good.
export function ModePicker({ lang, workspace, last, onPick }: Props) {
  const first = useRef<HTMLButtonElement | null>(null);

  // 1, 2, 3 pick a mode straight away
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)) return;
      const n = Number(e.key);
      if (n >= 1 && n <= CHAT_MODES.length) {
        e.preventDefault();
        onPick(CHAT_MODES[n - 1]);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onPick]);

  useEffect(() => { first.current?.focus({ preventScroll: true }); }, []);

  return (
    <div className="mode-picker">
      <div className="mode-picker-inner">
        <header className="mode-picker-head">
          <span className="eyebrow">{t("newSessionTitle", lang)}</span>
          <h1>{t("newSessionHeadline", lang)}</h1>
          <p>{t("newSessionLead", lang)}</p>
        </header>

        <div className="mode-cards" role="list">
          {CHAT_MODES.map((m, i) => (
            <button
              key={m}
              type="button"
              role="listitem"
              ref={i === 0 ? first : undefined}
              className={`mode-card mode-${m}`}
              onClick={() => onPick(m)}
            >
              <span className="mode-card-top">
                <span className="mode-card-icon"><Icon name={MODE_ICON[m]} size={20} /></span>
                {last === m && <span className="mode-card-tag">{t("modeLastUsed", lang)}</span>}
                <kbd className="mode-card-key">{i + 1}</kbd>
              </span>
              <span className="mode-card-title">{t(`mode_${m}`, lang)}</span>
              <span className="mode-card-hint">{t(`modeHint_${m}`, lang)}</span>
              <ul className="mode-card-points">
                {t(`modePoints_${m}`, lang).split("|").map((pt) => (
                  <li key={pt}><Icon name="check" size={12} />{pt}</li>
                ))}
              </ul>
              <span className="mode-card-go">
                {t("modeStart", lang)} <Icon name="arrowRight" size={13} />
              </span>
            </button>
          ))}
        </div>

        <footer className="mode-picker-foot">
          <span className="mode-picker-ws" title={workspace?.path}>
            <Icon name="folder" size={13} />
            {workspace ? workspace.name : t("noWorkspacePicked", lang)}
          </span>
          <span className="mode-picker-lock">
            <Icon name="lock" size={12} />
            {t("modeFixedNote", lang)}
          </span>
        </footer>
      </div>
    </div>
  );
}
