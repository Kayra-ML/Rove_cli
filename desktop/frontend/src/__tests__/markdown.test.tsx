import { describe, it, expect } from "vitest";
import { Markdown } from "../lib/markdown";
import { render } from "@testing-library/react";

describe("Markdown", () => {
  it("renders fenced code", () => {
    const { container } = render(<Markdown text={"```ts\nconst x = 1\n```"} />);
    expect(container.querySelector("pre")?.textContent).toContain("const x = 1");
  });

  it("renders bold and code inline", () => {
    const { container } = render(<Markdown text={"hello **world** and `x`"} />);
    expect(container.querySelector("strong")?.textContent).toBe("world");
    expect(container.querySelector("code")?.textContent).toBe("x");
  });
});
