import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { TermTranscript } from "../app/TermTranscript";

describe("terminal thinking", () => {
  afterEach(cleanup);
  it("shows the app's own thinking cloud, not a borrowed star spinner", () => {
    render(<TermTranscript banner={{ title: "Rove", line1: "", line2: "" }} blocks={[]} live={[]} streaming="" busy startedAt={Date.now()} tokens={0} />);
    const working = document.querySelector(".term-working")!;
    expect(working.querySelector(".think-cloud .think-cursor")).toBeTruthy();
    expect(working.textContent).not.toMatch(/[✢✳✶✻✽]/);
  });
});
