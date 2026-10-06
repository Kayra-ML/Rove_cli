import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { highlight } from "../lib/highlight";
import { Markdown } from "../lib/markdown";

const kinds = (code: string, lang: string) =>
  highlight(code, lang).filter((t) => t.kind !== "plain").map((t) => [t.kind, t.text]);
const whole = (code: string, lang = "") => highlight(code, lang).map((t) => t.text).join("");

describe("highlight", () => {
  it("never loses or changes a character", () => {
    for (const [code, lang] of [
      ['const x = "a\\"b"; // yorum', "ts"],
      ["# başlık\nkomut --flag 'tek'", "sh"],
      ["def f(x):\n    return x  # not", "py"],
      ["SELECT * FROM t -- son", "sql"],
      ["metin\tve\nsatırlar 42", ""],
    ] as const) {
      expect(whole(code, lang)).toBe(code);
    }
  });

  it("marks comments, strings, numbers, keywords, calls and types", () => {
    expect(kinds('func main() { s := "hi"; n := 42 } // not', "go")).toEqual([
      ["keyword", "func"], ["call", "main"], ["string", '"hi"'], ["number", "42"], ["comment", "// not"],
    ]);
    // a name before "(" is a call, a capitalised one a type
    expect(kinds("x := Server{}; run(x)", "go")).toEqual([["type", "Server"], ["call", "run"]]);
    // per language: # is a comment in Python, not in Go
    expect(kinds("# not\nprint('a')", "py")).toEqual([["comment", "# not"], ["keyword", "print"], ["string", "'a'"]]);
    expect(kinds("a # b", "go")).toEqual([]);
    // strings that run over lines close properly
    expect(kinds('"""doc\nstring"""\nx = 1', "py")).toEqual([["string", '"""doc\nstring"""'], ["number", "1"]]);
    // an unclosed quote stops at the line, so the rest is not swallowed
    expect(kinds("'oops\nreturn 1", "py")).toEqual([["string", "'oops"], ["keyword", "return"], ["number", "1"]]);
  });

  it("falls back for an unknown language instead of giving up", () => {
    expect(kinds('// hi\nreturn "x"', "brandnew")).toEqual([["comment", "// hi"], ["keyword", "return"], ["string", '"x"']]);
  });

  it("leaves very large code plain", () => {
    const big = "x = 1\n".repeat(9000);
    expect(highlight(big, "py")).toEqual([{ kind: "plain", text: big }]);
  });
});

describe("code blocks in a message", () => {
  afterEach(cleanup);
  const long = ["```python", ...Array.from({ length: 12 }, (_, i) => `print(${i})`), "```"].join("\n");

  it("folds a long block away, with an arrow that opens it", () => {
    render(<Markdown text={`bak:\n\n${long}`} />);
    expect(document.querySelector(".md-pre")).toBeNull();
    const head = screen.getByRole("button", { name: /python/ });
    expect(head.textContent).toContain("12 satır");
    expect(head.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(head);
    expect(head.getAttribute("aria-expanded")).toBe("true");
    expect(document.querySelector(".md-pre")?.textContent).toContain("print(0)");
    // and the code is coloured, not one flat colour
    expect(new Set(Array.from(document.querySelectorAll(".md-pre span")).map((e) => e.className)).size).toBeGreaterThan(1);
    fireEvent.click(head);
    expect(document.querySelector(".md-pre")).toBeNull();
  });

  it("shows a short block as it stands", () => {
    render(<Markdown text={"```sh\npip install numpy\npython matris.py\n```"} />);
    expect(document.querySelector(".md-pre")?.textContent).toContain("pip install numpy");
    expect(screen.getByRole("button", { name: /sh/ }).getAttribute("aria-expanded")).toBe("true");
  });
});
