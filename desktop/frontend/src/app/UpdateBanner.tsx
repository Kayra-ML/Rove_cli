import { useEffect, useState } from "react";
import { t, type Lang } from "~/lib/i18n";
import { openExternal } from "~/lib/platform";
import { toast } from "~/lib/toast";

type UpdateInfo = { current: string; latest: string; available: boolean; canInstall: boolean; notesUrl: string; error?: string };
type Updater = {
  CheckUpdate?: () => Promise<UpdateInfo>;
  InstallUpdate?: () => Promise<void>;
  UpdateInTerminal?: () => Promise<void>;
};

const updater = (): Updater | undefined => (window as unknown as { go?: { main?: { App?: Updater } } }).go?.main?.App;
const SKIP_KEY = "rove.update.skip";
const EVERY = 6 * 60 * 60 * 1000;

// UpdateBanner offers a newer release. The desktop app asks GitHub and
// compares versions itself (an older tag is not an update), and installs a
// release that carries the app in place: downloaded, checked against the
// release's checksums and signature, swapped in and restarted. A release
// without the app is installed from source in a terminal.
export function UpdateBanner({ lang }: { lang: Lang }) {
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [skip, setSkip] = useState(() => {
    try { return localStorage.getItem(SKIP_KEY) ?? ""; } catch { return ""; }
  });

  useEffect(() => {
    const up = updater();
    if (!up?.CheckUpdate) return;
    const check = () => { up.CheckUpdate!().then(setInfo).catch(() => {}); };
    const first = window.setTimeout(check, 8000);
    const id = window.setInterval(check, EVERY);
    return () => { window.clearTimeout(first); window.clearInterval(id); };
  }, []);

  if (!info?.available || info.latest === skip) return null;

  const install = async () => {
    const up = updater();
    setBusy(true);
    try {
      if (info.canInstall && up?.InstallUpdate) await up.InstallUpdate();
      else if (up?.UpdateInTerminal) {
        await up.UpdateInTerminal();
        toast(t("updTerminalOpened", lang), "ok");
      }
    } catch (e) {
      toast(`${t("updFailed", lang)}: ${e instanceof Error ? e.message : String(e)}`, "err");
    } finally {
      setBusy(false);
    }
  };
  const later = () => {
    try { localStorage.setItem(SKIP_KEY, info.latest); } catch { /* private window */ }
    setSkip(info.latest);
  };

  return (
    <div className="update-banner" role="status">
      <span>{t("updReady", lang).replace("{v}", info.latest)} <em className="upd-from">{info.current} →</em></span>
      {info.notesUrl && <button type="button" className="update-link" onClick={() => openExternal(info.notesUrl)}>{t("updNotes", lang)}</button>}
      <button type="button" className="primary upd-go" disabled={busy} onClick={() => void install()}>
        {busy ? t("updInstalling", lang) : info.canInstall ? t("updInstall", lang) : t("updInTerminal", lang)}
      </button>
      <button type="button" className="update-dismiss" onClick={later} title={t("updLater", lang)}>×</button>
    </div>
  );
}
