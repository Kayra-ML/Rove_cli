// Colours for fenced code. Not a parser: a scanner that finds the pieces a
// reader looks for — comments, strings, numbers, keywords, call names and
// type names — and leaves everything else plain. A per-language table gives
// it the comment markers, quote characters and words; an unknown language
// falls back to a C-like profile, which is wrong about little and right
// about most of it.

export type TokenKind = "comment" | "string" | "number" | "keyword" | "call" | "type" | "plain";
export interface Token {
  kind: TokenKind;
  text: string;
}

// Beyond this, code is shown plain: colouring it costs more than it helps.
const MAX_CHARS = 40000;

interface Syntax {
  line: string[]; // line comment markers
  block: [string, string][]; // block comment pairs
  quotes: string[]; // string delimiters that end at the line's end
  spans: string[]; // string delimiters that run across lines
  words: Set<string>;
  // In these languages a capitalised name is a type, not a variable.
  caps: boolean;
}

const set = (s: string) => new Set(s.split(/\s+/).filter(Boolean));

const C_WORDS = `if else for while do switch case default break continue return
  function const let var new delete typeof instanceof this null true false undefined
  class extends implements interface enum import export from as async await
  try catch finally throw yield static public private protected void sizeof struct union`;

const LANGS: Record<string, Syntax> = {
  js: {
    line: ["//"], block: [["/*", "*/"]], quotes: ["'", '"'], spans: ["`"], caps: true,
    words: set(`${C_WORDS} of keyof readonly namespace declare satisfies infer never unknown any
      string number boolean symbol bigint object type require module exports`),
  },
  go: {
    line: ["//"], block: [["/*", "*/"]], quotes: ['"', "'"], spans: ["`"], caps: true,
    words: set(`package import func var const type struct interface map chan go defer select
      return if else for range switch case default break continue fallthrough goto
      nil true false iota make new len cap append copy delete panic recover
      string int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64
      float32 float64 bool byte rune error any`),
  },
  python: {
    line: ["#"], block: [], quotes: ["'", '"'], spans: ['"""', "'''"], caps: true,
    words: set(`def class return if elif else for while in is not and or import from as pass
      break continue try except finally raise with lambda yield global nonlocal assert del
      None True False self async await match case print len range str int float bool
      list dict set tuple open type isinstance super`),
  },
  rust: {
    line: ["//"], block: [["/*", "*/"]], quotes: ['"', "'"], spans: [], caps: true,
    words: set(`fn let mut const static struct enum impl trait for while loop if else match
      return use mod pub crate self super as in ref move where unsafe async await dyn type
      Some None Ok Err true false i8 i16 i32 i64 u8 u16 u32 u64 usize isize f32 f64
      bool char str String Vec Option Result Box`),
  },
  shell: {
    line: ["#"], block: [], quotes: ["'", '"'], spans: [], caps: false,
    words: set(`if then else elif fi for while until do done case esac in function return exit
      local export source echo cd ls rm mv cp mkdir cat grep sed awk curl wget sudo set
      trap read printf test true false shift eval exec kill pwd chmod chown tar git npm go`),
  },
  sql: {
    line: ["--"], block: [["/*", "*/"]], quotes: ["'", '"'], spans: [], caps: false,
    words: set(`select from where join left right inner outer full on group by order having
      limit offset insert into values update set delete create table drop alter add column
      primary key foreign references index view as and or not null is distinct
      count sum avg min max case when then else end union all exists between like asc desc`),
  },
  json: { line: [], block: [], quotes: ['"'], spans: [], caps: false, words: set("true false null") },
  yaml: { line: ["#"], block: [], quotes: ["'", '"'], spans: [], caps: false, words: set("true false null yes no on off") },
  html: { line: [], block: [["<!--", "-->"]], quotes: ['"', "'"], spans: [], caps: false, words: set("") },
  css: { line: [], block: [["/*", "*/"]], quotes: ['"', "'"], spans: [], caps: false, words: set("important from to") },
};

const ALIAS: Record<string, string> = {
  javascript: "js", jsx: "js", ts: "js", tsx: "js", typescript: "js", mjs: "js", cjs: "js",
  java: "js", c: "js", cpp: "js", "c++": "js", cs: "js", csharp: "js", kotlin: "js", swift: "js", php: "js",
  golang: "go",
  py: "python", python3: "python",
  rs: "rust",
  sh: "shell", bash: "shell", zsh: "shell", console: "shell", terminal: "shell", shellscript: "shell",
  yml: "yaml", toml: "yaml", ini: "yaml", conf: "yaml", dockerfile: "shell", makefile: "shell",
  xml: "html", svg: "html", vue: "html",
  scss: "css", less: "css",
  postgres: "sql", postgresql: "sql", mysql: "sql", sqlite: "sql",
};

// syntaxFor is the profile for a fence's language tag.
function syntaxFor(lang: string): Syntax {
  const key = lang.trim().toLowerCase();
  return LANGS[ALIAS[key] ?? key] ?? LANGS.js;
}

const isWordStart = (c: string) => /[A-Za-z_$]/.test(c);
const isWord = (c: string) => /[A-Za-z0-9_$]/.test(c);
const isDigit = (c: string) => c >= "0" && c <= "9";

// highlight splits code into coloured pieces, in order; joining their text
// gives the input back unchanged.
export function highlight(code: string, lang = ""): Token[] {
  if (code.length > MAX_CHARS) return [{ kind: "plain", text: code }];
  const s = syntaxFor(lang);
  const out: Token[] = [];
  let plain = "";
  const flush = () => {
    if (plain) {
      out.push({ kind: "plain", text: plain });
      plain = "";
    }
  };
  const push = (kind: TokenKind, text: string) => {
    flush();
    out.push({ kind, text });
  };
  const at = (i: number, tok: string) => code.startsWith(tok, i);

  let i = 0;
  while (i < code.length) {
    const c = code[i];

    const lineTok = s.line.find((m) => at(i, m));
    if (lineTok) {
      let j = code.indexOf("\n", i);
      if (j < 0) j = code.length;
      push("comment", code.slice(i, j));
      i = j;
      continue;
    }

    const block = s.block.find(([open]) => at(i, open));
    if (block) {
      const close = code.indexOf(block[1], i + block[0].length);
      const j = close < 0 ? code.length : close + block[1].length;
      push("comment", code.slice(i, j));
      i = j;
      continue;
    }

    const span = s.spans.find((q) => at(i, q));
    if (span) {
      let j = i + span.length;
      while (j < code.length && !at(j, span)) j += code[j] === "\\" ? 2 : 1;
      j = Math.min(code.length, j + span.length);
      push("string", code.slice(i, j));
      i = j;
      continue;
    }

    if (s.quotes.includes(c)) {
      let j = i + 1;
      while (j < code.length && code[j] !== c && code[j] !== "\n") j += code[j] === "\\" ? 2 : 1;
      j = Math.min(code.length, code[j] === c ? j + 1 : j);
      push("string", code.slice(i, j));
      i = j;
      continue;
    }

    if (isDigit(c) || (c === "." && isDigit(code[i + 1] ?? ""))) {
      let j = i;
      while (j < code.length && /[0-9a-fA-FxXoObB._]/.test(code[j])) j += 1;
      push("number", code.slice(i, j));
      i = j;
      continue;
    }

    if (isWordStart(c)) {
      let j = i;
      while (j < code.length && isWord(code[j])) j += 1;
      const word = code.slice(i, j);
      let after = j;
      while (after < code.length && (code[after] === " " || code[after] === "\t")) after += 1;
      if (s.words.has(word)) push("keyword", word);
      else if (code[after] === "(") push("call", word);
      else if (s.caps && /^[A-Z]/.test(word)) push("type", word);
      else plain += word;
      i = j;
      continue;
    }

    plain += c;
    i += 1;
  }
  flush();
  return out;
}
