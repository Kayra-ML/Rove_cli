import { useEffect, useState } from "react";
import { rpc } from "~/lib/rpc";
import { t, type Lang } from "~/lib/i18n";

// the API level this app needs from its daemon (pkg/protocol.APILevel)
// the daemon methods this build of the app calls; the tests read it too,
// so raising it in one place keeps them honest
export const REQUIRED_API = 18;

// ApiBanner warns when the daemon is older than the app — its newer
// features (fetching models, reviewing changes…) would quietly not work.
export function ApiBanner({ lang }: { lang: Lang }) {
  const [level, setLevel] = useState<number | null>(null);
  const [hidden, setHidden] = useState(false);
  useEffect(() => {
    rpc<{ apiLevel?: number }>("usage.get")
      .then((h) => setLevel(h?.apiLevel ?? 0))
      .catch(() => setLevel(null));
  }, []);
  if (hidden || level === null || level >= REQUIRED_API) return null;
  return (
    <div className="update-banner api-banner" role="alert">
      <span>{t("apiOld", lang).replace("{have}", String(level)).replace("{need}", String(REQUIRED_API))}</span>
      <button className="update-dismiss" onClick={() => setHidden(true)} title={t("close", lang)}>×</button>
    </div>
  );
}
