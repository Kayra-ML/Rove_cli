import type { Session } from "~/lib/types";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { Icon } from "./Icons";

interface Props {
  // the subagent's channel that is open over the chat
  channel: Session;
  onClose: () => void;
}

// TeamBar sits over the message box while a subagent's channel is open in
// Orchestra: it says whose work is on screen and leads back to the chat.
// Orchestra has no standing cast — the chat splits its work between plain
// subagents — so there is nothing to show while the chat itself is open.
export function TeamBar({ channel, onClose }: Props) {
  const { lang } = usePrefs();
  return (
    <section className="team" aria-label={t("subagentChannel", lang)}>
      <button type="button" className="team-row" onClick={onClose} title={t("backToChat", lang)}>
        <Icon name="home" size={13} />
        <strong>{t("backToChat", lang)}</strong>
      </button>
      <div className="team-row on">
        <span className="team-main">
          <Icon name="agents" size={12} />
          <strong>{channel.title}</strong>
        </span>
        <button type="button" className="team-x" title={t("close", lang)} onClick={onClose}>×</button>
      </div>
    </section>
  );
}
