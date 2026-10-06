import { useState } from "react";
import type { Session, Workspace } from "~/lib/types";
import type { Space } from "~/lib/spaces";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { CodeMap } from "./CodeMap";
import { ContextMap } from "./ContextMap";
import { SessionContextMap } from "./SessionContextMap";
import { Icon } from "./Icons";

export type AutoTab = "context" | "sessions" | "code";
const MAP_SPACE_KEY = "aether.automation.space";

interface Props {
  tab: AutoTab;
  onTab: (t: AutoTab) => void;
  workspace: Workspace | null;
  // the Chat space's open chat: the context map's default share target
  sessionId?: string;
  onOpenSession: (s: Session) => void;
}

function loadMapSpace(): Space {
  try { return localStorage.getItem(MAP_SPACE_KEY) === "office" ? "office" : "chat"; } catch { return "chat"; }
}

// AutomationPage is the Automation space: a map on the left and one sidebar
// on the right, top to bottom. The sidebar picks the map (Context Map: what
// one chat's context is made of; Session Map: chats linked with cables; Code
// Map: a project folder's code as a graph), then whose conversations to list
// (Agents or Sessions), then lists them.
export function AutomationPage({ tab, onTab, workspace, sessionId, onOpenSession }: Props) {
  const { lang } = usePrefs();
  const [mapSpace, setMapSpaceState] = useState<Space>(loadMapSpace);
  // a chat picked on the session map to see its context
  const [focus, setFocus] = useState<string | undefined>();
  const setMapSpace = (s: Space) => {
    setMapSpaceState(s);
    try { localStorage.setItem(MAP_SPACE_KEY, s); } catch { /* private */ }
  };

  const sideTop = (
    <div className="auto-picks">
      <div className="seg auto-maps" role="tablist" aria-label={t("spaceAutomation", lang)}>
        {([["context", "contextMap", "graph"], ["sessions", "sessionMap", "cable"], ["code", "codeMap", "terminal"]] as const).map(([id, key, icon]) => (
          <button key={id} type="button" role="tab" aria-selected={tab === id} aria-label={t(key, lang)} title={t(key, lang)} className={tab === id ? "on" : ""} onClick={() => onTab(id)}>
            <Icon name={icon} size={13} /> {t(`${key}Short`, lang)}
          </button>
        ))}
      </div>
      <div className="seg auto-space" role="radiogroup" aria-label={t("convs", lang)}>
        {(["office", "chat"] as Space[]).map((sp) => (
          <button key={sp} type="button" role="radio" aria-checked={mapSpace === sp} className={mapSpace === sp ? "on" : ""} onClick={() => setMapSpace(sp)}>
            {t(sp === "office" ? "sideAgents" : "sessions", lang)}
          </button>
        ))}
      </div>
    </div>
  );

  return (
    <div className="panel center-panel flush auto-page">
      {tab === "context"
        ? <SessionContextMap key={mapSpace} sessionId={focus ?? sessionId} space={mapSpace} sideTop={sideTop} onOpenSession={onOpenSession} />
        : tab === "code"
        ? <CodeMap workspace={workspace} sessionId={sessionId} space={mapSpace} sideTop={sideTop} />
        : <ContextMap key={mapSpace} space={mapSpace} onOpenSession={onOpenSession} sideTop={sideTop} onShowContext={(id) => { setFocus(id); onTab("context"); }} />}
    </div>
  );
}
