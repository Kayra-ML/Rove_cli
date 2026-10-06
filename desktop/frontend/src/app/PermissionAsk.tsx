import { useCallback, useEffect, useState } from "react";
import { rpc, subscribeEvents } from "~/lib/rpc";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import { Icon } from "./Icons";

// Ask is one waiting question: what the agent wants to do, and on what.
export interface Ask {
  id: string;
  sessionId?: string;
  action: string;
  tool: string;
  detail?: string;
}

// PermissionAsk is the question an agent's run waits on: a rule left this
// call open, so the user decides. It sits over the middle of the window —
// the run is stopped until it is answered, so it should not be missed —
// and the answer may be kept as a rule.
export function PermissionAsk() {
  const { lang } = usePrefs();
  const [queue, setQueue] = useState<Ask[]>([]);
  const [busy, setBusy] = useState(false);
  const ask = queue[0] ?? null;

  const drop = useCallback((id: string) => setQueue((q) => q.filter((x) => x.id !== id)), []);

  // questions the app missed while it was closed or reloading
  useEffect(() => {
    rpc<Ask[]>("permission.asks").then((list) => setQueue(list ?? [])).catch(() => {});
  }, []);

  useEffect(() => {
    const add = (ev: { payload?: unknown }) => {
      const p = (ev.payload ?? {}) as Ask;
      if (p.id) setQueue((q) => (q.some((x) => x.id === p.id) ? q : [...q, p]));
    };
    // answered elsewhere, or the question gave up waiting
    const gone = (ev: { payload?: unknown }) => {
      const p = (ev.payload ?? {}) as { id?: string };
      if (p.id) drop(p.id);
    };
    const offs = [subscribeEvents("permission.ask", add), subscribeEvents("permission.answered", gone)];
    return () => offs.forEach((f) => f());
  }, [drop]);

  const answer = useCallback(async (allow: boolean, remember = false) => {
    if (!ask || busy) return;
    setBusy(true);
    try {
      await rpc("permission.answer", { id: ask.id, allow, remember });
    } catch {
      // the question timed out or another window answered it
    } finally {
      setBusy(false);
      drop(ask.id);
    }
  }, [ask, busy, drop]);

  // Escape refuses: the safe answer, and the run gets on with it
  useEffect(() => {
    if (!ask) return;
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") void answer(false);
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, [ask, answer]);

  if (!ask) return null;
  const action = t(`perm_${ask.action}`, lang);
  return (
    <div className="modal-backdrop perm-backdrop">
      <div className="perm-ask" role="alertdialog" aria-modal="true" aria-label={t("permAskTitle", lang)}>
        <div className="perm-head">
          <span className="perm-icon"><Icon name="key" size={15} /></span>
          <div>
            <strong>{t("permAskTitle", lang)}</strong>
            <span>{t("permAskLead", lang).replace("{action}", action)}</span>
          </div>
        </div>
        <div className="perm-what">
          <span className="perm-tool">{ask.tool}</span>
          {ask.detail && <code>{ask.detail}</code>}
        </div>
        {queue.length > 1 && <p className="perm-queue">{t("permAskMore", lang).replace("{n}", String(queue.length - 1))}</p>}
        <div className="perm-actions">
          <button type="button" className="ghost" disabled={busy} onClick={() => void answer(false)}>{t("permDeny", lang)}</button>
          <span className="tw-spacer" />
          <button type="button" className="ghost" disabled={busy} onClick={() => void answer(true, true)}>{t("permAlways", lang)}</button>
          <button type="button" className="primary" disabled={busy} onClick={() => void answer(true)}>{t("permAllow", lang)}</button>
        </div>
      </div>
    </div>
  );
}
