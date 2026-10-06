// Window chrome hints for CSS: on macOS the title bar is hidden and the
// traffic lights sit over the app's own header, except in full screen, where
// macOS hides them (and shows its own title bar on hover).
export function bootPlatform(root: HTMLElement = document.documentElement) {
  const mac = /Mac/i.test(navigator.platform || navigator.userAgent);
  root.classList.toggle("platform-mac", mac);
  const sync = () =>
    root.classList.toggle("is-fullscreen", window.innerWidth >= screen.width && window.innerHeight >= screen.height);
  sync();
  window.addEventListener("resize", sync);
  return () => window.removeEventListener("resize", sync);
}

// openExternal opens a web page in the system browser: the desktop app asks
// the shell (a link inside the window would open in the window), a plain
// browser opens a tab.
export function openExternal(url: string) {
  const rt = (window as unknown as { runtime?: { BrowserOpenURL?: (u: string) => void } }).runtime;
  if (rt?.BrowserOpenURL) {
    rt.BrowserOpenURL(url);
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}
