import { useCallback, useEffect, useMemo, useState } from "react";
import { rpc } from "~/lib/rpc";
import { toast } from "~/lib/toast";
import { openExternal } from "~/lib/platform";
import { t, type Lang } from "~/lib/i18n";
import type { Provider } from "~/lib/types";

// One entry of the daemon's connection catalog, with what it found here.
export type Connection = {
  id: string;
  name: string;
  kind: "agent" | "api" | "local";
  blurb: string;
  bin?: string;
  install?: string;
  login?: string;
  baseUrl?: string;
  needKey?: boolean;
  keyUrl?: string;
  docs?: string;
  models?: string[];
  installed?: boolean;
  version?: string;
  loggedIn?: boolean;
  account?: string;
};

interface Props {
  lang: Lang;
  providers: Provider[];
  onDone: () => void | Promise<void>;
  onClose: () => void;
}

const GROUPS: { kind: Connection["kind"]; key: string }[] = [
  { kind: "agent", key: "conGroupAgent" },
  { kind: "api", key: "conGroupApi" },
  { kind: "local", key: "conGroupLocal" },
];

// ConnectPicker is "add a provider" as a list of the systems a user can
// connect their own account to: agent systems on this computer (Claude Code,
// Codex, Antigravity, Hermes) that sign in through the browser, model APIs
// that take a key, and local servers. It says what is installed and signed
// in, opens the terminal for an install or a sign-in, and the browser for a
// key page.
export function ConnectPicker({ lang, providers, onDone, onClose }: Props) {
  const [list, setList] = useState<Connection[] | null>(null);
  const [query, setQuery] = useState("");
  const [pick, setPick] = useState<Connection | null>(null);
  const [checking, setChecking] = useState(false);

  const load = useCallback(async () => {
    setChecking(true);
    try {
      const l = await rpc<Connection[]>("connect.catalog");
      setList(l ?? []);
      setPick((p) => (p ? (l ?? []).find((x) => x.id === p.id) ?? p : p));
    } catch (e) {
      toast(e instanceof Error ? e.message : "catalog failed", "err");
      setList([]);
    } finally {
      setChecking(false);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);

  // back from the terminal or the browser: look again
  useEffect(() => {
    const onFocus = () => { if (pick?.kind === "agent") void load(); };
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [pick, load]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (list ?? []).filter((c) => !q || `${c.name} ${c.blurb} ${c.id}`.toLowerCase().includes(q));
  }, [list, query]);

  const connected = (c: Connection) =>
    providers.some((p) => (c.kind === "agent" ? p.kind === "agent-cli" && p.baseUrl.startsWith(`agent://${c.id}?`) : p.baseUrl === c.baseUrl));

  return (
    <div className="con">
      {!pick ? (
        <>
          <div className="con-top">
            <input autoFocus className="con-search" value={query} placeholder={t("conSearch", lang)} onChange={(e) => setQuery(e.target.value)} />
            <button type="button" className="ghost" onClick={onClose}>{t("cancel", lang)}</button>
          </div>
          {list === null && <div className="map-muted">{t("conChecking", lang)}</div>}
          {GROUPS.map((g) => {
            const items = shown.filter((c) => c.kind === g.kind);
            if (items.length === 0) return null;
            return (
              <section key={g.kind} className="con-group">
                <div className="section-label">{t(g.key, lang)}</div>
                <div className="con-grid">
                  {items.map((c) => (
                    <button key={c.id} type="button" className={`con-card ${c.kind}`} onClick={() => setPick(c)}>
                      <span className="con-body">
                        <strong>{c.name}</strong>
                        <span className="con-state">
                          {connected(c) ? <em className="ok">{t("conConnected", lang)}</em>
                            : c.kind === "agent" ? (c.installed ? (c.loggedIn === false ? <em className="warn">{t("conNotSignedIn", lang)}</em> : <em className="ok">{t("conInstalled", lang)}</em>) : <em>{t("conNotInstalled", lang)}</em>)
                            : c.needKey ? <em>{t("conNeedsKey", lang)}</em> : <em>{t("conNoKey", lang)}</em>}
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              </section>
            );
          })}
        </>
      ) : pick.kind === "agent" ? (
        <AgentSetup c={pick} lang={lang} checking={checking} onRecheck={() => void load()} onBack={() => setPick(null)} onDone={onDone} />
      ) : (
        <ApiSetup c={pick} lang={lang} onBack={() => setPick(null)} onDone={onDone} />
      )}
    </div>
  );
}

function AgentSetup({ c, lang, checking, onRecheck, onBack, onDone }: {
  c: Connection; lang: Lang; checking: boolean; onRecheck: () => void; onBack: () => void; onDone: () => void | Promise<void>;
}) {
  const [access, setAccess] = useState<"edits" | "full">("edits");
  const [busy, setBusy] = useState(false);
  const step = async (s: "install" | "login") => {
    try {
      await rpc("connect.terminal", { id: c.id, step: s });
      toast(t(s === "install" ? "conInstallOpened" : "conLoginOpened", lang), "ok");
    } catch (e) {
      toast(e instanceof Error ? e.message : "failed", "err");
    }
  };
  const add = async () => {
    setBusy(true);
    try {
      await rpc("connect.add", { id: c.id, access });
      toast(`${c.name}: ${t("conAdded", lang)}`, "ok");
      await onDone();
    } catch (e) {
      toast(e instanceof Error ? e.message : "failed", "err");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="con-setup">
      <button type="button" className="ghost con-back" onClick={onBack}>← {t("conBack", lang)}</button>
      <div className="con-head">
        <div>
          <h3>{c.name}</h3>
          <p>{c.blurb}</p>
        </div>
      </div>

      <ol className="con-steps">
        <li className={c.installed ? "done" : "now"}>
          <div className="con-step-main">
            <strong>{t("conStepInstall", lang)}</strong>
            <span>{c.installed ? `${t("conInstalled", lang)}${c.version ? ` · ${c.version}` : ""}` : c.install ? <code>{c.install}</code> : t("conInstallDocs", lang)}</span>
          </div>
          {!c.installed && (c.install
            ? <button type="button" onClick={() => void step("install")}>{t("conRunInTerminal", lang)}</button>
            : c.docs && <button type="button" onClick={() => openExternal(c.docs!)}>{t("conOpenDocs", lang)} ↗</button>)}
        </li>
        <li className={!c.installed ? "" : c.loggedIn === true ? "done" : "now"}>
          <div className="con-step-main">
            <strong>{t("conStepLogin", lang)}</strong>
            <span>
              {c.loggedIn === true ? `${t("conSignedIn", lang)}${c.account ? ` · ${c.account}` : ""}`
                : c.loggedIn === false ? t("conSignInHint", lang)
                : t("conSignInUnknown", lang)}
            </span>
          </div>
          {c.installed && c.loggedIn !== true && <button type="button" onClick={() => void step("login")}>{t("conSignIn", lang)} ↗</button>}
        </li>
        <li className={c.installed ? "now" : ""}>
          <div className="con-step-main">
            <strong>{t("conStepAccess", lang)}</strong>
            <div className="seg con-access" role="radiogroup">
              {(["edits", "full"] as const).map((a) => (
                <button key={a} type="button" role="radio" aria-checked={access === a} className={access === a ? "on" : ""} onClick={() => setAccess(a)}>
                  {t(a === "edits" ? "conAccessEdits" : "conAccessFull", lang)}
                </button>
              ))}
            </div>
            <span className="con-hint">{t(access === "edits" ? "conAccessEditsHint" : "conAccessFullHint", lang)}</span>
          </div>
        </li>
      </ol>

      <div className="con-actions">
        <button type="button" className="ghost" disabled={checking} onClick={onRecheck}>{checking ? t("conChecking", lang) : `↻ ${t("conRecheck", lang)}`}</button>
        {c.docs && <button type="button" className="ghost" onClick={() => openExternal(c.docs!)}>{t("conOpenDocs", lang)} ↗</button>}
        <button type="button" className="primary" disabled={!c.installed || busy} onClick={() => void add()}>
          {busy ? "…" : t("conConnect", lang)}
        </button>
      </div>
    </div>
  );
}

function ApiSetup({ c, lang, onBack, onDone }: { c: Connection; lang: Lang; onBack: () => void; onDone: () => void | Promise<void> }) {
  const [key, setKey] = useState("");
  const [base, setBase] = useState(c.baseUrl ?? "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      const secretId = `${c.id}-key`;
      if (key.trim()) await rpc("secret.put", { id: secretId, value: key.trim() });
      const r = await rpc<Provider & { modelsError?: string }>("provider.upsert", { name: c.name, kind: "openai-compat", baseUrl: base.trim(), secretId, models: [] });
      if (r.modelsError) {
        setErr(`${t("provModelsFailed", lang)}: ${r.modelsError}`);
        return;
      }
      toast(`${c.name}: ${r.models?.length ?? 0} ${t("provModelsAdded", lang)}`, "ok");
      await onDone();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="con-setup">
      <button type="button" className="ghost con-back" onClick={onBack}>← {t("conBack", lang)}</button>
      <div className="con-head">
        <div>
          <h3>{c.name}</h3>
          <p>{c.blurb}</p>
        </div>
      </div>
      <div className="form-stack">
        <label>{t("providerBaseUrl", lang)}</label>
        <input value={base} onChange={(e) => setBase(e.target.value)} />
        {c.needKey && (
          <>
            <label>{t("providerApiKey", lang)}</label>
            <div className="con-key">
              <input type="password" autoComplete="off" placeholder="sk-…" value={key} onChange={(e) => setKey(e.target.value)} />
              {c.keyUrl && <button type="button" onClick={() => openExternal(c.keyUrl!)}>{t("conGetKey", lang)} ↗</button>}
            </div>
            <span className="con-hint">{t("conKeyHint", lang)}</span>
          </>
        )}
        {err && <div className="prov-err">{err}</div>}
        <div className="con-actions">
          {c.docs && <button type="button" className="ghost" onClick={() => openExternal(c.docs!)}>{t("conOpenDocs", lang)} ↗</button>}
          <button type="button" className="primary" disabled={busy || (c.needKey && !key.trim())} onClick={() => void save()}>
            {busy ? t("provFetching", lang) : t("conConnect", lang)}
          </button>
        </div>
      </div>
    </div>
  );
}
