import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { forgetReads, rpc, subscribeEvents } from "../lib/rpc";
import { pollWhileVisible } from "../lib/poll";

// Over a tunnel every call is a round trip, so the same question asked
// twice in the same breath is answered once — and a poll fires only while
// someone is looking.

const calls: string[] = [];
let answer: unknown = { n: 1 };
const bridge = {
  RPC: vi.fn(async (method: string) => {
    calls.push(method);
    await new Promise((r) => setTimeout(r, 5));
    return JSON.stringify({ ok: true, result: answer });
  }),
  Token: vi.fn(async () => "t"),
  HTTPAddr: vi.fn(async () => "127.0.0.1:7420"),
};

// a stand-in for the daemon's event stream, so events can be pushed by hand
const opened: { push: (ev: unknown) => void }[] = [];
class FakeStream {
  onmessage: ((m: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() {
    opened.push({ push: (ev) => this.onmessage?.({ data: JSON.stringify(ev) }) });
  }
  close() {}
}

describe("rpc sharing", () => {
  beforeEach(() => {
    calls.length = 0;
    answer = { n: 1 };
    forgetReads();
    opened.length = 0;
    Object.assign(window, { go: { main: { App: bridge } }, EventSource: FakeStream });
  });
  afterEach(() => { forgetReads(); });

  it("asks once when several parts want the same thing at once", async () => {
    const out = await Promise.all([
      rpc("session.list"), rpc("session.list"), rpc("session.list"),
      rpc("workspace.rules", { id: "w" }), rpc("workspace.rules", { id: "w" }),
      rpc("workspace.rules", { id: "other" }),
    ]);
    expect(calls).toEqual(["session.list", "workspace.rules", "workspace.rules"]);
    expect(out[0]).toEqual({ n: 1 });
    // each holder gets its own copy: changing one must not change another
    (out[0] as { n: number }).n = 99;
    expect(out[1]).toEqual({ n: 1 });
  });

  it("does not share a call that changes something, and forgets what it knew", async () => {
    await rpc("session.list");
    await rpc("session.send", { content: "x" });   // a write
    answer = { n: 2 };
    expect(await rpc("session.list")).toEqual({ n: 2 }); // asked again
    expect(calls).toEqual(["session.list", "session.send", "session.list"]);
  });

  it("forgets what it knew when the daemon says something happened", async () => {
    const stop = subscribeEvents("*", () => {});
    await vi.waitFor(() => expect(opened.length).toBe(1));
    await rpc("session.list");
    answer = { n: 2 };

    // a reply being written changes nothing that is asked for by name
    opened[0].push({ type: "message.delta", payload: { delta: "a" } });
    expect(await rpc("session.list")).toEqual({ n: 1 });

    // but anything else means the answer held here is from before it
    opened[0].push({ type: "session.updated", payload: {} });
    expect(await rpc("session.list")).toEqual({ n: 2 });
    stop();
  });

  it("does not remember a failure", async () => {
    bridge.RPC.mockImplementationOnce(async () => { calls.push("agent.list"); throw new Error("down"); });
    await expect(rpc("agent.list")).rejects.toThrow("down");
    await rpc("agent.list");
    expect(calls).toEqual(["agent.list", "agent.list"]);
  });
});

describe("polling", () => {
  let hidden = false;
  beforeEach(() => {
    vi.useFakeTimers();
    hidden = false;
    Object.defineProperty(document, "hidden", { get: () => hidden, configurable: true });
  });
  afterEach(() => vi.useRealTimers());

  it("runs while the window is watched, stops while it is not, catches up on return", () => {
    const run = vi.fn();
    const stop = pollWhileVisible(run, 1000);
    expect(run).toHaveBeenCalledTimes(1);   // once, straight away
    vi.advanceTimersByTime(2000);
    expect(run).toHaveBeenCalledTimes(3);

    hidden = true;                           // the window goes away
    vi.advanceTimersByTime(10000);
    expect(run).toHaveBeenCalledTimes(3);    // nothing while nobody looks

    hidden = false;                          // and comes back
    document.dispatchEvent(new Event("visibilitychange"));
    expect(run).toHaveBeenCalledTimes(4);    // once, to be fresh again
    stop();
    vi.advanceTimersByTime(5000);
    expect(run).toHaveBeenCalledTimes(4);
  });
});
