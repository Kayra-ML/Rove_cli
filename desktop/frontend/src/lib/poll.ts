// Polling that respects a window nobody is looking at. Every poll is a
// round trip; on a server reached over an SSH tunnel a handful of timers
// keep the link busy around the clock for a screen no one can see. These
// ones stop while the window is hidden and catch up the moment it comes
// back, so what you see is still fresh when you look.

export function pollWhileVisible(run: () => void, everyMs: number): () => void {
  let last = 0;
  const fire = () => {
    last = Date.now();
    run();
  };
  fire();
  const id = window.setInterval(() => {
    if (!document.hidden) fire();
  }, everyMs);
  // coming back to a window that has been away for longer than the period
  // means the screen is stale: ask once, now, rather than waiting it out
  const wake = () => {
    if (!document.hidden && Date.now() - last >= everyMs) fire();
  };
  document.addEventListener("visibilitychange", wake);
  return () => {
    window.clearInterval(id);
    document.removeEventListener("visibilitychange", wake);
  };
}
