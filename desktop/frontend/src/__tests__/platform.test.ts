import { afterEach, describe, expect, it } from "vitest";
import { bootPlatform } from "~/lib/platform";

const setNav = (platform: string) => Object.defineProperty(navigator, "platform", { value: platform, configurable: true });
const setSize = (w: number, h: number, sw: number, sh: number) => {
  Object.defineProperty(window, "innerWidth", { value: w, configurable: true });
  Object.defineProperty(window, "innerHeight", { value: h, configurable: true });
  Object.defineProperty(screen, "width", { value: sw, configurable: true });
  Object.defineProperty(screen, "height", { value: sh, configurable: true });
};

describe("window chrome classes", () => {
  let stop: (() => void) | undefined;
  afterEach(() => stop?.());

  it("marks macOS and follows full screen", () => {
    const root = document.createElement("html");
    setNav("MacIntel");
    setSize(1440, 860, 1512, 982);
    stop = bootPlatform(root);
    expect(root.classList.contains("platform-mac")).toBe(true);
    expect(root.classList.contains("is-fullscreen")).toBe(false);
    setSize(1512, 982, 1512, 982);
    window.dispatchEvent(new Event("resize"));
    expect(root.classList.contains("is-fullscreen")).toBe(true);
  });

  it("leaves other platforms alone", () => {
    const root = document.createElement("html");
    setNav("Win32");
    stop = bootPlatform(root);
    expect(root.classList.contains("platform-mac")).toBe(false);
  });
});
