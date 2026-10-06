import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(
  resolve(__dirname, "../styles/global.css"),
  "utf8",
);
const app = readFileSync(
  resolve(__dirname, "../app/App.tsx"),
  "utf8",
);

describe("desktop side panels", () => {
  it("shell: the sidebar is open by default, resizable, and leaves the grid when closed; no right panel", () => {
    expect(app).toContain('readFlag("aether.shell.side", true)');
    // the left rail belongs to Office and Chat; full pages (Automation, Market) drop it
    expect(app).toContain("const showSide = sideOpen && space !== null;");
    expect(app).toContain("{showSide && !sideOver && (");
    expect(app).toContain("gridTemplateColumns: columns(showSide && !sideOver ? sideW : null, null)");
    // too narrow for two columns: the list opens over the page instead
    expect(app).toContain("const sideOver = showSide && !fitsSide(winW);");
    expect(css).toMatch(/\.shell-body\.side-over > \.shell-side\s*\{[^}]*position:\s*absolute/);
    expect(app).toContain("onPointerDown={startDrag}");
    // files and snapshots are gone; an open subagent channel's bar sits over the message box
    expect(app).not.toContain("RightPanel");
    expect(app).toContain("<TeamBar channel={channel}");
    expect(css).toMatch(/\.gutter\s*\{[^}]*cursor:\s*col-resize/);
  });
});

// .center-panel stretches every direct child (flex: 1). A bar that sits
// above the content must opt out, or it takes an equal share of the height
// (the Automation header once took half the page this way).
describe("center panel children", () => {
  const rule = (sel: string) => {
    const m = css.match(new RegExp(`(^|\\n)${sel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{([^}]*)\\}`));
    return m ? m[2] : "";
  };
  it("stretches content but not the bars above it", () => {
    expect(rule(".center-panel > *")).toMatch(/flex:\s*1/);
    expect(rule(".center-panel > .market-head")).not.toBe("");
  });
  it("Teamwork: orchestras scroll on the left, the conversation panel stands on the right", () => {
    // the cable to the panel is drawn against the Teamwork page, not the window
    expect(css).toMatch(/\.tw \{ position: relative; display: flex;/);
    expect(css).toMatch(/\.tw-stage \{[^}]*overflow: auto;/);
    expect(rule(".tw-panel")).toMatch(/flex: 0 0 clamp\(/);
    // in the panel the request box is part of its column, not floating over it
    expect(rule(".tw-panel .tw-dock")).toMatch(/position: relative;[^}]*transform: none;/);
    // a narrow window stacks the panel under the orchestras
    expect(css).toMatch(/@media \(max-width: 860px\) \{\s*\.tw \{ flex-direction: column;/);
  });

  // One rhythm down the transcript: the parts of an answer sit close, and a
  // new question opens a gap, so a turn's end is plain without a divider.
  it("spaces a turn's parts closely and opens a gap before the next question", () => {
    const inner = Number(rule(".transcript").match(/gap:\s*(\d+)px/)?.[1]);
    const before = Number(rule(".turn-user").match(/margin-top:\s*(\d+)px/)?.[1]);
    expect(inner).toBeGreaterThan(0);
    expect(before).toBeGreaterThan(inner);
    expect(css).toMatch(/\.transcript > \.turn-user:first-child \{ margin-top: 0; \}/);
    // the pieces inside a turn are spaced by their container, not by
    // margins of their own that would stack with it
    expect(rule(".edit-card")).not.toMatch(/margin:/);
    expect(rule(".tool-chips")).toMatch(/margin:\s*\d+px 0 0/);
    expect(css).toMatch(/\.md > :last-child \{ margin-bottom: 0; \}/);
  });

  it("gives the Automation maps the whole page, with their sidebar full height", () => {
    expect(rule(".auto-page > .map, .auto-page > .cmap")).toMatch(/flex:\s*1/);
    expect(css).not.toMatch(/\.auto-head\s*\{/);
  });
});
