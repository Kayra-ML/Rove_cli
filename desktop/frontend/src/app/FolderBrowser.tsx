import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { rpc, setRemoteFolderPicker } from "~/lib/rpc";
import { t, type Lang } from "~/lib/i18n";
import { Icon } from "./Icons";
import { useDialog } from "~/hooks/useDialog";

type DirList = { path: string; parent: string; home: string; dirs: string[]; project: boolean };

// FolderBrowser picks a project folder on the machine the daemon runs on —
// a server's folders, which the native picker cannot see.
function FolderBrowser({ lang, onDone }: { lang: Lang; onDone: (path: string) => void }) {
  const sheet = useDialog<HTMLDivElement>();
  const [list, setList] = useState<DirList | null>(null);
  const [typed, setTyped] = useState("");
  const [err, setErr] = useState("");
  const go = useCallback(async (path: string) => {
    try {
      const l = await rpc<DirList>("fs.dirs", { path });
      setList(l);
      setTyped(l.path);
      setErr("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }, []);
  useEffect(() => { void go(""); }, [go]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onDone(""); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onDone]);

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onDone(""); }}>
      <div className="folder-browser" role="dialog" aria-modal="true" aria-label={t("folderPick", lang)} ref={sheet}>
        <header className="folder-head">
          <h2>{t("folderPick", lang)}</h2>
          <span className="map-muted">{t("folderOnServer", lang)}</span>
          <span className="tw-spacer" />
          <button type="button" className="tree-icon" title={t("close", lang)} onClick={() => onDone("")}>×</button>
        </header>
        <form className="folder-path" onSubmit={(e) => { e.preventDefault(); void go(typed); }}>
          <button type="button" className="tree-icon" title={t("folderUp", lang)} disabled={!list?.parent} onClick={() => list?.parent && void go(list.parent)}>↑</button>
          <button type="button" className="tree-icon" title="~" onClick={() => void go(list?.home ?? "")}><Icon name="home" size={13} /></button>
          <input value={typed} onChange={(e) => setTyped(e.target.value)} spellCheck={false} aria-label={t("folderPath", lang)} />
        </form>
        {err && <div className="conn-err">{err}</div>}
        <div className="folder-list">
          {list && list.dirs.length === 0 && <p className="map-muted">{t("folderEmpty", lang)}</p>}
          {list?.dirs.map((d) => (
            <button key={d} type="button" className="folder-row" onDoubleClick={() => onDone(`${list.path.replace(/\/$/, "")}/${d}`)} onClick={() => void go(`${list.path.replace(/\/$/, "")}/${d}`)}>
              <Icon name="folder" size={14} /> {d}
            </button>
          ))}
        </div>
        <footer className="folder-foot">
          <code title={list?.path}>{list?.path}</code>
          {list?.project && <span className="folder-project">{t("folderProject", lang)}</span>}
          <span className="tw-spacer" />
          <button type="button" className="ghost" onClick={() => onDone("")}>{t("cancel", lang)}</button>
          <button type="button" className="primary" disabled={!list} onClick={() => list && onDone(list.path)}>{t("folderOpen", lang)}</button>
        </footer>
      </div>
    </div>,
    document.body,
  );
}

// FolderBrowserHost answers pickFolder() while the app runs on a server.
export function FolderBrowserHost({ lang }: { lang: Lang }) {
  const [pending, setPending] = useState<((path: string) => void) | null>(null);
  useEffect(() => {
    setRemoteFolderPicker(() => new Promise<string>((resolve) => setPending(() => resolve)));
    return () => setRemoteFolderPicker(null);
  }, []);
  if (!pending) return null;
  return <FolderBrowser lang={lang} onDone={(path) => { pending(path); setPending(null); }} />;
}
