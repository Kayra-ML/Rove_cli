import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { Thinking } from "../app/Thinking";

const chat = readFileSync(resolve(__dirname, "../app/Chat.tsx"), "utf8");
const css = readFileSync(resolve(__dirname, "../styles/global.css"), "utf8");

describe("Thinking", () => {
  it("says what the agent is doing, and says it to a screen reader too", () => {
    const { container } = render(<Thinking lang="tr" />);
    const row = container.querySelector("[role=status]")!;
    expect(row.getAttribute("aria-live")).toBe("polite");
    expect(container.querySelector(".think-word")?.textContent).toBe("düşünüyor…");
  });

  it("prefers the live word over the standing one", () => {
    const { container } = render(<Thinking label="yazıyor…" lang="tr" />);
    expect(container.querySelector(".think-word")?.textContent).toBe("yazıyor…");
  });

  it("carries the cloud with its cursor and sweep", () => {
    const { container } = render(<Thinking lang="tr" />);
    expect(container.querySelector(".think-cloud img")).toBeTruthy();
    expect(container.querySelector(".think-cursor")).toBeTruthy();
    expect(container.querySelector(".think-wave")).toBeTruthy();
  });

  it("clips the icon to the cloud and holds still when motion is unwanted", () => {
    // the icon and the sweep share one box and one outline, so the light
    // stays inside the cloud
    expect(css).toMatch(/\.think-cloud img,\s*\n\.think-wave \{[^}]*clip-path:\s*polygon/);
    expect(css).toMatch(/prefers-reduced-motion:\s*reduce\)\s*\{\s*\.think-cursor, \.think-wave/);
  });
});

describe("waiting for an answer", () => {
  // The daemon can take seconds to answer; before this the message you had
  // just sent was not on the page at all while you waited for it.
  it("shows the question straight away, before the daemon hands it back", () => {
    expect(chat).toContain("setPendingAsk({ text: content");
    expect(chat).toMatch(/const echoed = pendingAsk && !shownMessages\.some/);
  });

  it("puts the cloud under the question while the agent works", () => {
    expect(chat).toContain("{busy && !streaming && <Thinking label={live} lang={lang} />}");
  });

  // Without room below it, a new question sits on the bottom edge under the
  // message box with nowhere to scroll to.
  it("keeps room under the last question so it can rise off the floor", () => {
    expect(chat).toContain('<div className="thread-tail" style={{ height: tail }} />');
    expect(chat).toMatch(/vp\.clientHeight \* \(1 - RISE\) - below/);
  });
});
