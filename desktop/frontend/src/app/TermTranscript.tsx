import { useEffect, useState } from "react";
import { Markdown } from "~/lib/markdown";
import { formatCount, type Block, type ToolView } from "~/lib/transcript";
import { usePrefs } from "~/hooks/usePrefs";
import { t } from "~/lib/i18n";
import logo from "~/assets/logo-256.png";
import { ThinkCloud } from "./Thinking";


type Banner = { title: string; line1: string; line2: string };

interface Props {
  banner: Banner;
  blocks: Block[];
  live: Block[];
  streaming: string;
  busy: boolean;
  startedAt: number | null;
  tokens: number;
}

// TermTranscript draws a session the way a coding CLI does: prompt rows,
// bullets for replies and tool calls, diffs and clipped command output.
export function TermTranscript({ banner, blocks, live, streaming, busy, startedAt, tokens }: Props) {
  return (
    <div className="term-log">
      <div className="term-banner">
        <img src={logo} alt="" />
        <div>
          <div className="term-banner-title">{banner.title}</div>
          <div className="term-dim">{banner.line1}</div>
          <div className="term-dim">{banner.line2}</div>
        </div>
      </div>
      {blocks.map((b) => <BlockView key={b.id} b={b} />)}
      {live.map((b) => <BlockView key={b.id} b={b} />)}
      {streaming && (
        <div className="term-row">
          <span className="term-bullet on">●</span>
          <div className="term-text"><Markdown text={streaming} /><span className="term-caret">▍</span></div>
        </div>
      )}
      {busy && <Working startedAt={startedAt} tokens={tokens} />}
    </div>
  );
}

function BlockView({ b }: { b: Block }) {
  switch (b.kind) {
    case "user":
      return <div className="term-user"><span className="term-prompt">&gt;</span><span>{b.text}</span></div>;
    case "notice":
      return (
        <div className="term-row term-notice">
          <span className="term-bullet">⇄</span>
          <div>
            <div className="term-dim">{b.title}</div>
            {b.files.length > 0 && <div className="term-sub"><span className="term-elbow">⎿</span>{b.files.join(", ")}</div>}
          </div>
        </div>
      );
    case "text":
      return (
        <div className="term-row">
          <span className="term-bullet">●</span>
          <div className="term-text"><Markdown text={b.text} /></div>
        </div>
      );
    case "tool":
      return <ToolRow tv={b.tool} />;
  }
}

function ToolRow({ tv }: { tv: ToolView }) {
  const { lang } = usePrefs();
  const state = tv.pending ? "pending" : tv.isError ? "err" : "ok";
  return (
    <div className="term-row">
      <span className={`term-bullet ${state}`}>●</span>
      <div className="term-tool">
        <div><strong>{tv.verb}</strong>{tv.arg ? <span className="term-arg">({tv.arg})</span> : <span className="term-arg">()</span>}</div>
        {(tv.summary || tv.pending) && (
          <div className={`term-sub${tv.isError ? " err" : ""}`}>
            <span className="term-elbow">⎿</span>
            {tv.pending && !tv.summary ? t("termRunning", lang) : tv.summary}
          </div>
        )}
        {tv.diff && tv.diff.length > 0 && (
          <div className="term-diff">
            {tv.diff.map((d, i) => (
              <div key={i} className={`term-diff-line ${d.sign === "+" ? "add" : d.sign === "-" ? "del" : ""}`}>
                <span className="term-diff-n">{d.n ?? ""}</span>
                <span className="term-diff-s">{d.sign}</span>
                <span className="term-diff-t">{d.text}</span>
              </div>
            ))}
          </div>
        )}
        {tv.output && tv.output.length > 0 && (
          <div className="term-out">
            {tv.output.map((l, i) => <div key={i}>{l || " "}</div>)}
          </div>
        )}
        {tv.more ? <div className="term-dim term-more">… +{tv.more} {t("termLines", lang)}</div> : null}
      </div>
    </div>
  );
}

function Working({ startedAt, tokens }: { startedAt: number | null; tokens: number }) {
  const { lang } = usePrefs();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(id);
  }, []);
  const secs = startedAt ? Math.max(0, Math.floor((now - startedAt) / 1000)) : 0;
  // the app's own mark, thinking — the cloud with its blinking cursor
  return (
    <div className="term-working" role="status" aria-live="polite">
      <ThinkCloud width={18} />
      <span>{t("termWorking", lang)}</span>
      <span className="term-dim"> ({t("termEsc", lang)} · {secs}s · ↓ {formatCount(tokens)} token)</span>
    </div>
  );
}
