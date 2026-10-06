import { Fragment, useState } from "react";
import type { ToolCall } from "~/lib/types";
import { describeTool, type ToolOutcome, type ToolView } from "~/lib/transcript";
import { t, type Lang } from "~/lib/i18n";
import { Icon } from "./Icons";

// FOLD is how many plain calls a turn shows before folding them into one
// line. Edits never fold: they are the part you came to read.
const FOLD = 3;

interface Props {
  calls: ToolCall[];
  results: Map<string, ToolOutcome>;
  lang: Lang;
}

// ToolRows is a turn's tool calls: a quiet one-line log for the ones that
// only looked around, and an open card for every file the agent wrote, with
// the added and removed lines as they will land.
export function ToolRows({ calls, results, lang }: Props) {
  const [open, setOpen] = useState(false);
  const rows = calls.map((tc) => describeTool(tc.id, tc.name, tc.argsJson, results.get(tc.id)));
  const plain = rows.filter((r) => !r.diff?.length);
  const folded = !open && plain.length > FOLD;
  let seen = 0;
  return (
    <div className="tool-chips">
      {rows.map((tv) => {
        if (tv.diff?.length) return <EditCard key={tv.id} tv={tv} lang={lang} />;
        seen += 1;
        if (folded && seen > 1) return null;
        return (
          <div
            key={tv.id}
            className={`tool-chip${tv.pending ? " pending" : tv.refused ? " refused" : tv.isError ? " err" : ""}`}
            title={[tv.arg, tv.isError ? results.get(tv.id)?.content ?? "" : ""].filter(Boolean).join("\n\n")}
          >
            <Icon name="terminal" size={12} />
            <strong>{tv.verb}</strong>
            {tv.arg && <span className="tool-chip-arg">{tv.arg}</span>}
            {folded ? (
              <button type="button" className="tool-more" onClick={() => setOpen(true)}>
                + {plain.length - 1} {t("toolMore", lang)}
              </button>
            ) : (
              tv.summary && <span className="tool-chip-sum">{tv.summary}</span>
            )}
          </div>
        );
      })}
      {open && plain.length > FOLD && (
        <button type="button" className="tool-more tool-less" onClick={() => setOpen(false)}>{t("toolLess", lang)}</button>
      )}
    </div>
  );
}

// EditCard is one written file: its name and how much it gained or lost,
// then the lines themselves. It stays shut until asked — the header already
// says which file changed and by how much, and a long diff opened by itself
// fills the screen and buries the answer under it. The caret opens it, and
// the body scrolls rather than pushing the rest of the turn away.
function EditCard({ tv, lang }: { tv: ToolView; lang: Lang }) {
  const [open, setOpen] = useState(false);
  const name = tv.arg.split("/").pop() || tv.arg;
  return (
    <div className={`edit-card${open ? " open" : ""}${tv.isError ? " err" : ""}`}>
      <button type="button" className="edit-head" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <Icon name="copy" size={12} />
        <span className="edit-path" title={tv.arg}>{name}</span>
        {(tv.added ?? 0) > 0 && <span className="edit-add">+{tv.added}</span>}
        {(tv.removed ?? 0) > 0 && <span className="edit-del">−{tv.removed}</span>}
        <span className="edit-caret" aria-hidden>›</span>
      </button>
      {open && (
        <div className="edit-body">
          {/* the inner strip is as wide as the longest line, so every row
              stretches the whole scrollable width and its colour reaches the
              far edge instead of stopping where the box happens to end */}
          <div className="edit-lines">
            {tv.diff!.map((l, i) => (
              <Fragment key={i}>
                <div className={`edit-line${l.sign === "+" ? " add" : l.sign === "-" ? " del" : ""}`}>
                  <span className="edit-sign" aria-hidden>{l.sign === " " ? "" : l.sign}</span>
                  <code>{l.text || " "}</code>
                </div>
              </Fragment>
            ))}
            {tv.more ? <div className="edit-more">+{tv.more} {t("codeLines", lang)}</div> : null}
          </div>
        </div>
      )}
    </div>
  );
}
