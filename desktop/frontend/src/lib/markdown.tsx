import { useState, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import { highlight } from "./highlight";
import { t } from "./i18n";

// The answers an agent writes are ordinary Markdown: numbered steps, nested
// lists, quotes, rules, task boxes, tables. Parsing that by hand is a long
// tail of rules that are easy to get subtly wrong — emphasis alone turned
// "4 * 5 * 6" into italics — so the parsing is remark's (CommonMark + GFM)
// and what stays ours is the part that belongs to this app: a fenced block
// is a foldable card with the theme's own colours, never a wall of text.

// OPEN is how many lines a block may have and still be shown as it stands:
// a couple of lines are already small, while a script is folded away until
// it is asked for.
const OPEN = 6;

// CodeBlock is one fenced block: a header line saying what it is, and the
// code itself — coloured, and folded away when it is long.
function CodeBlock({ lang, code }: { lang: string; code: string }) {
  const lines = code.split("\n");
  const [open, setOpen] = useState(lines.length <= OPEN);
  const label = lang.trim() || t("code");
  return (
    <div className={`md-code-block${open ? " open" : ""}`}>
      <button type="button" className="code-head" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span className="code-caret" aria-hidden>›</span>
        <span className="code-lang">{label}</span>
        <span className="code-lines">{lines.length} {t("codeLines")}</span>
      </button>
      {open && (
        <pre className="md-pre" data-lang={lang || undefined}>
          <code>
            {highlight(code, lang).map((tok, n) =>
              tok.kind === "plain" ? tok.text : <span key={n} className={`tok-${tok.kind}`}>{tok.text}</span>,
            )}
          </code>
        </pre>
      )}
    </div>
  );
}

// text pulls the plain source back out of a fenced block's children, which
// remark hands over as nested nodes rather than as a string.
function text(node: ReactNode): string {
  if (node == null || node === false || node === true) return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(text).join("");
  const el = node as { props?: { children?: ReactNode } };
  return el.props ? text(el.props.children) : "";
}

const components: Components = {
  // A fenced block becomes our card; an inline span stays a plain <code>.
  // remark marks the difference by putting the block inside a <pre>, so the
  // pre is dropped here and rebuilt by CodeBlock.
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children, ...rest }) => {
    const fence = /language-(\S+)/.exec(className ?? "");
    const body = text(children);
    // remark keeps a fenced block's trailing newline; a card should not
    // show it as an empty last line
    if (fence || body.includes("\n")) {
      return <CodeBlock lang={fence ? fence[1] : ""} code={body.replace(/\n$/, "")} />;
    }
    return <code className="md-code" {...rest}>{children}</code>;
  },
  p: ({ children }) => <p className="md-p">{children}</p>,
  h1: ({ children }) => <h1 className="md-h md-h1">{children}</h1>,
  h2: ({ children }) => <h2 className="md-h md-h2">{children}</h2>,
  h3: ({ children }) => <h3 className="md-h md-h3">{children}</h3>,
  h4: ({ children }) => <h4 className="md-h md-h4">{children}</h4>,
  h5: ({ children }) => <h5 className="md-h md-h5">{children}</h5>,
  h6: ({ children }) => <h6 className="md-h md-h6">{children}</h6>,
  ul: ({ children, className }) => <ul className={`md-ul${className?.includes("contains-task-list") ? " md-tasks" : ""}`}>{children}</ul>,
  ol: ({ children, start }) => <ol className="md-ol" start={start ?? undefined}>{children}</ol>,
  li: ({ children }) => <li>{children}</li>,
  // a task box is read, never set: the answer already happened
  input: ({ checked, type }) => (type === "checkbox" ? <input className="md-task" type="checkbox" checked={!!checked} readOnly /> : null),
  blockquote: ({ children }) => <blockquote className="md-quote">{children}</blockquote>,
  hr: () => <hr className="md-hr" />,
  table: ({ children }) => <div className="md-table-wrap"><table className="md-table">{children}</table></div>,
  a: ({ href, children }) => (
    <a className="md-a" href={href} target="_blank" rel="noreferrer noopener">{children}</a>
  ),
  // an image in an answer is usually a broken remote URL; show what it was
  img: ({ alt, src }) => <code className="md-code">{alt || String(src ?? "")}</code>,
};

export function Markdown({ text: source }: { text: string }) {
  return (
    <div className="md">
      {/* breaks: in a chat a line the agent broke is a line the reader sees
          broken, which plain CommonMark would join into one paragraph */}
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkBreaks]} components={components}>
        {source}
      </ReactMarkdown>
    </div>
  );
}
