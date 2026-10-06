import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ConnState } from "~/lib/rpc";
import type { SSHTarget } from "~/lib/types";
import { canConnectRemote, connectRemote, connectionState, disconnectRemote, onConnectProgress, onConnection } from "~/lib/connection";
import { t, type Lang } from "~/lib/i18n";
import { loadSSHHosts } from "./Popovers";
import { Icon } from "./Icons";

// ConnectionMenu is the title bar's "where Rove runs" switch: this
// computer, or one of the saved servers (Rove is set up there on first use).
export function ConnectionMenu({ lang, onAddServer }: { lang: Lang; onAddServer: () => void }) {
  const [st, setSt] = useState<ConnState>({ status: "local" });
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<string | null>(null); // host being connected
  const [step, setStep] = useState("");
  const [err, setErr] = useState("");
  const btnRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [at, setAt] = useState<{ right: number; top: number } | null>(null);

  useEffect(() => {
    void connectionState().then(setSt);
    const offs = [onConnection(setSt), onConnectProgress(setStep)];
    return () => offs.forEach((f) => f());
  }, []);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      const n = e.target as Node;
      if (menuRef.current?.contains(n) || btnRef.current?.contains(n)) return;
      setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  if (!canConnectRemote()) return null;

  const remote = st.status !== "local" && st.status !== "error";
  const label = remote ? st.host ?? "" : t("connLocal", lang);
  const toggle = () => {
    if (open) { setOpen(false); return; }
    const r = btnRef.current?.getBoundingClientRect();
    if (r) setAt({ right: Math.max(8, window.innerWidth - r.right), top: r.bottom + 6 });
    setErr("");
    setOpen(true);
  };
  const go = async (h: SSHTarget) => {
    setBusy(h.host);
    setErr("");
    setStep("");
    try {
      await connectRemote(h); // reloads on success
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
      setBusy(null);
    }
  };
  const hosts = loadSSHHosts();

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        className={`conn-pill ${st.status}`}
        title={st.status === "reconnecting" ? t("connReconnecting", lang) : t("connTitle", lang)}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={toggle}
      >
        <span className={`conn-dot ${st.status}`} />
        <Icon name={remote ? "terminal" : "workspace"} size={13} />
        <span className="conn-label">{label}</span>
        <span className="profile-caret">⌄</span>
      </button>
      {open && at && createPortal(
        <div className="pane-menu conn-menu" role="menu" ref={menuRef} style={{ right: at.right, top: at.top }}>
          <div className="modes-head">{t("connTitle", lang)}</div>
          <button type="button" role="menuitemradio" aria-checked={!remote} className={!remote ? "on" : ""} disabled={busy !== null} onClick={() => { if (remote) void disconnectRemote(); else setOpen(false); }}>
            <Icon name="workspace" size={14} />
            <span className="modes-text"><strong>{t("connLocal", lang)}</strong><span>{t("connLocalHint", lang)}</span></span>
            {!remote && <Icon name="check" size={12} />}
          </button>
          {hosts.map((h) => {
            const name = h.user ? `${h.user}@${h.host}` : h.host;
            const on = remote && (st.host === name || st.host === h.host);
            return (
              <button key={name} type="button" role="menuitemradio" aria-checked={on} className={on ? "on" : ""} disabled={busy !== null} onClick={() => { if (!on) void go(h); }}>
                <Icon name="terminal" size={14} />
                <span className="modes-text">
                  <strong>{name}</strong>
                  <span>{busy === h.host ? (step || t("connConnecting", lang)) : on ? (st.status === "reconnecting" ? t("connReconnecting", lang) : t("connOnServer", lang)) : (h.hostName ?? t("connRunHere", lang))}</span>
                </span>
                {on && <Icon name="check" size={12} />}
              </button>
            );
          })}
          {err && <div className="conn-err">{err}</div>}
          <button type="button" className="conn-add" onClick={() => { setOpen(false); onAddServer(); }}>+ {t("connAddServer", lang)}</button>
        </div>,
        document.body,
      )}
    </>
  );
}

// ConnectionBanner says when the server connection dropped and is coming back.
export function ConnectionBanner({ lang }: { lang: Lang }) {
  const [st, setSt] = useState<ConnState>({ status: "local" });
  useEffect(() => {
    void connectionState().then(setSt);
    return onConnection(setSt);
  }, []);
  if (st.status !== "reconnecting") return null;
  return <div className="conn-banner" role="status">{t("connReconnecting", lang)} · {st.host}{st.message ? ` (${st.message})` : ""}</div>;
}
