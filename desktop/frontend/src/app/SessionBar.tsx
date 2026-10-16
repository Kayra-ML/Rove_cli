import { t, type Lang } from "~/lib/i18n";
import type { ChatMode } from "~/lib/spaces";
import type { Session, Workspace } from "~/lib/types";
import { Icon } from "./Icons";
import { MODE_ICON } from "./ModePicker";

type Props = {
  lang: Lang;
  mode: ChatMode;
  session: Session | null;
  workspace: Workspace | null;
};

// SessionBar heads a Chat-space session: the mode it runs in — picked with
// the + menu and never changed, which the lock says — what it is about, and
// where it works.
export function SessionBar({ lang, mode, session, workspace }: Props) {
  const draft = !session;
  return (
    <div className={`session-bar mode-${mode}`}>
      <span
        className="mode-chip"
        title={t("modeLockedNote", lang)}
      >
        <Icon name={MODE_ICON[mode]} size={13} />
        {t(`mode_${mode}`, lang)}
        <span className="mode-chip-lock" aria-label={t("modeLockedNote", lang)}><Icon name="lock" size={11} /></span>
      </span>
      <span className="session-bar-title" title={session?.title}>
        {draft ? t("newSessionTitle", lang) : session.title || t("chat", lang)}
      </span>
      {draft && <span className="session-bar-note">{t("modeDraftNote", lang)}</span>}
      <span className="session-bar-spacer" />
      {/* the root folder ("/") is no project: nothing worth naming */}
      {workspace && workspace.name && workspace.name !== "/" && workspace.path !== "/" && (
        <span className="session-bar-ws" title={workspace.path}>
          <Icon name="folder" size={12} />
          {workspace.name}
        </span>
      )}
    </div>
  );
}
