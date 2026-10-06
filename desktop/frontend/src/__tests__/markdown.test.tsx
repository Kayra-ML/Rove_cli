import { describe, it, expect } from "vitest";
import { Markdown } from "../lib/markdown";
import { render } from "@testing-library/react";

describe("Markdown", () => {
  it("renders fenced code", () => {
    const { container } = render(<Markdown text={"```ts\nconst x = 1\n```"} />);
    expect(container.querySelector("pre")?.textContent).toContain("const x = 1");
    expect(container.querySelector(".code-lang")?.textContent).toBe("ts");
  });

  it("folds a long fenced block and leaves a short one open", () => {
    const long = "```py\n" + Array.from({ length: 30 }, (_, n) => `x = ${n}`).join("\n") + "\n```";
    const { container } = render(<Markdown text={long} />);
    expect(container.querySelector("pre")).toBeNull();
    expect(container.querySelector(".code-head")?.getAttribute("aria-expanded")).toBe("false");
  });

  it("keeps an inline code span inline", () => {
    const { container } = render(<Markdown text={"call `run()` first"} />);
    expect(container.querySelector(".md-code")?.textContent).toBe("run()");
    expect(container.querySelector(".md-code-block")).toBeNull();
  });

  it("renders bold, italic and strikethrough", () => {
    const { container } = render(<Markdown text={"**a** and *b* and ~~c~~"} />);
    expect(container.querySelector("strong")?.textContent).toBe("a");
    expect(container.querySelector("em")?.textContent).toBe("b");
    expect(container.querySelector("del")?.textContent).toBe("c");
  });

  // The hand-written parser used to italicise the 5 in "4 * 5 * 6".
  it("leaves arithmetic alone", () => {
    const { container } = render(<Markdown text={"4 * 5 * 6 = 120"} />);
    expect(container.querySelector("em")).toBeNull();
    expect(container.textContent).toContain("4 * 5 * 6 = 120");
  });

  it("honours a backslash escape", () => {
    const { container } = render(<Markdown text={"\\*not bold\\*"} />);
    expect(container.querySelector("em")).toBeNull();
    expect(container.textContent).toBe("*not bold*");
  });

  it("renders an ordered list as an ordered list", () => {
    const { container } = render(<Markdown text={"1. bir\n2. iki\n3. üç"} />);
    const items = [...container.querySelectorAll("ol > li")];
    expect(items.map((li) => li.textContent)).toEqual(["bir", "iki", "üç"]);
  });

  it("nests a list inside a list", () => {
    const { container } = render(<Markdown text={"- a\n  - a1\n  - a2\n- b"} />);
    const nested = container.querySelector("ul ul");
    expect(nested).toBeTruthy();
    expect([...nested!.querySelectorAll("li")].map((li) => li.textContent)).toEqual(["a1", "a2"]);
  });

  it("renders a task list with read-only boxes", () => {
    const { container } = render(<Markdown text={"- [x] bitti\n- [ ] duruyor"} />);
    const boxes = [...container.querySelectorAll<HTMLInputElement>("input[type=checkbox]")];
    expect(boxes.map((b) => b.checked)).toEqual([true, false]);
    expect(boxes.every((b) => b.readOnly)).toBe(true);
  });

  it("renders a blockquote and a rule", () => {
    const { container } = render(<Markdown text={"> dikkat\n> ikinci satır\n\n---\n\nson"} />);
    expect(container.querySelector("blockquote")?.textContent).toContain("dikkat");
    expect(container.querySelector("hr")).toBeTruthy();
  });

  it("renders headings down to the sixth level", () => {
    const { container } = render(<Markdown text={"# a\n## b\n### c\n#### d\n##### e\n###### f"} />);
    expect(["h1", "h2", "h3", "h4", "h5", "h6"].map((h) => container.querySelector(h)?.textContent))
      .toEqual(["a", "b", "c", "d", "e", "f"]);
  });

  it("opens links in a new tab without leaking the referrer", () => {
    const { container } = render(<Markdown text={"[x](https://example.com)"} />);
    const a = container.querySelector("a")!;
    expect(a.getAttribute("href")).toBe("https://example.com");
    expect(a.getAttribute("rel")).toContain("noopener");
  });

  it("links a bare URL", () => {
    const { container } = render(<Markdown text={"see https://example.com/x for more"} />);
    expect(container.querySelector("a")?.getAttribute("href")).toBe("https://example.com/x");
  });

  // A table used to fall through to the paragraph branch and come out as a
  // line of pipes, which read like raw output rather than an answer.
  it("renders a table as a table, with alignment and inline marks", () => {
    const { container } = render(
      <Markdown text={"| Ad | Sayı |\n|:---|---:|\n| **a** | 1 |\n| b | 2 |"} />,
    );
    const table = container.querySelector("table");
    expect(table).toBeTruthy();
    const heads = [...table!.querySelectorAll("th")];
    expect(heads.map((h) => h.textContent)).toEqual(["Ad", "Sayı"]);
    expect(heads[0].style.textAlign).toBe("left");
    expect(heads[1].style.textAlign).toBe("right");
    const rows = [...table!.querySelectorAll("tbody tr")];
    expect(rows).toHaveLength(2);
    expect(rows[0].querySelector("strong")?.textContent).toBe("a");
    expect([...rows[1].querySelectorAll("td")].map((c) => c.textContent)).toEqual(["b", "2"]);
  });

  it("renders a one-column table", () => {
    const { container } = render(<Markdown text={"| Tek |\n|---|\n| a |\n| b |"} />);
    expect([...container.querySelectorAll("tbody td")].map((c) => c.textContent)).toEqual(["a", "b"]);
  });

  it("reads a pipe written \\| as text, not a cell break", () => {
    const { container } = render(<Markdown text={"| a | b |\n|---|---|\n| x \\| y | 2 |"} />);
    const cells = [...container.querySelectorAll("tbody td")].map((c) => c.textContent);
    expect(cells).toEqual(["x | y", "2"]);
  });

  // In a chat, a line the agent broke is a line the reader sees broken;
  // plain CommonMark would join the two into one paragraph.
  it("keeps a single newline as a line break", () => {
    const { container } = render(<Markdown text={"birinci satır\nikinci satır"} />);
    expect(container.querySelectorAll("br")).toHaveLength(1);
    expect(container.querySelectorAll("p")).toHaveLength(1);
  });

  it("leaves a paragraph of pipes alone when there is no rule line", () => {
    const { container } = render(<Markdown text={"a | b and more text"} />);
    expect(container.querySelector("table")).toBeNull();
    expect(container.querySelector("p")?.textContent).toBe("a | b and more text");
  });
});
