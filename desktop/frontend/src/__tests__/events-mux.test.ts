import { afterEach, describe, expect, it, vi } from "vitest";

// All subscriptions share one EventSource: browsers allow only 6 connections
// per host, and a stream per subscriber left the rest pending forever.

class FakeES {
  static all: FakeES[] = [];
  onmessage: ((m: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor(public url: string) {
    FakeES.all.push(this);
  }
  close() {
    this.closed = true;
  }
  emit(ev: object) {
    this.onmessage?.({ data: JSON.stringify(ev) });
  }
}

async function load() {
  vi.resetModules();
  FakeES.all = [];
  vi.stubGlobal("EventSource", FakeES);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) }));
  return import("../lib/rpc");
}
const settle = () => new Promise((r) => setTimeout(r, 0));

describe("event subscriptions", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("share one stream and fan out by the daemon's filter rules", async () => {
    const { subscribeEvents } = await load();
    const got: Record<string, string[]> = { type: [], topic: [], prefix: [], all: [] };
    const offs = [
      subscribeEvents("run.done", (e) => got.type.push(e.type)),
      subscribeEvents("session.s1", (e) => got.topic.push(e.type)),
      subscribeEvents("tool.*", (e) => got.prefix.push(e.type)),
      subscribeEvents("", (e) => got.all.push(e.type)),
    ];
    for (let i = 0; i < 12; i++) subscribeEvents("message.delta", () => {});
    await settle();
    expect(FakeES.all).toHaveLength(1);
    expect(FakeES.all[0].url).not.toContain("filter=");

    const es = FakeES.all[0];
    es.emit({ type: "run.done", topic: "session.s2" });
    es.emit({ type: "message.delta", topic: "session.s1" });
    es.emit({ type: "tool.start", topic: "session.s3" });
    expect(got.type).toEqual(["run.done"]);
    expect(got.topic).toEqual(["message.delta"]);
    expect(got.prefix).toEqual(["tool.start"]);
    expect(got.all).toHaveLength(3);

    // unsubscribed handlers stop hearing events
    offs[0]();
    es.emit({ type: "run.done" });
    expect(got.type).toHaveLength(1);
  });

  it("closes when the last subscriber leaves and reconnects after an error", async () => {
    vi.useFakeTimers();
    try {
      const { subscribeEvents } = await load();
      const off = subscribeEvents("x", () => {});
      await vi.runAllTimersAsync();
      expect(FakeES.all).toHaveLength(1);
      FakeES.all[0].onerror?.();
      await vi.advanceTimersByTimeAsync(1000);
      expect(FakeES.all).toHaveLength(2);
      off();
      expect(FakeES.all[1].closed).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("a throwing handler does not starve the others", async () => {
    const { subscribeEvents } = await load();
    const seen: string[] = [];
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    subscribeEvents("", () => { throw new Error("boom"); });
    subscribeEvents("", (e) => seen.push(e.type));
    await settle();
    FakeES.all[0].emit({ type: "a" });
    expect(seen).toEqual(["a"]);
    err.mockRestore();
  });
});

// A daemon that numbers its events: the stream comes back where it was.
class ResumingES {
  static all: ResumingES[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((m: { data: string; lastEventId?: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  listeners: Record<string, ((m: { data: string }) => void)[]> = {};
  constructor(public url: string) {
    ResumingES.all.push(this);
  }
  addEventListener(type: string, fn: (m: { data: string }) => void) {
    (this.listeners[type] ??= []).push(fn);
  }
  close() {
    this.closed = true;
  }
  hello(resumed: boolean) {
    this.onopen?.();
    for (const fn of this.listeners.hello ?? []) fn({ data: JSON.stringify({ epoch: "e", seq: 2, resumed }) });
  }
  emit(ev: object, id: string) {
    this.onmessage?.({ data: JSON.stringify(ev), lastEventId: id });
  }
}

async function loadResuming() {
  vi.resetModules();
  ResumingES.all = [];
  vi.stubGlobal("EventSource", ResumingES);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) }));
  return import("../lib/rpc");
}

describe("a stream that comes back", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("asks for what came after its last event, and re-reads nothing when it gets it", async () => {
    vi.useFakeTimers();
    const { subscribeEvents, onReconnect } = await loadResuming();
    const seen: string[] = [];
    subscribeEvents("", (e) => seen.push(e.type));
    const reread = vi.fn();
    onReconnect(reread);
    await vi.advanceTimersByTimeAsync(0);
    const first = ResumingES.all[0];
    expect(first.url).not.toContain("since=");
    first.hello(false);
    first.emit({ type: "a" }, "e.1");
    first.emit({ type: "b" }, "e.2");
    first.onerror?.();
    await vi.advanceTimersByTimeAsync(1000);
    const second = ResumingES.all[1];
    expect(second.url).toContain("since=e.2");
    second.hello(true);
    second.emit({ type: "c" }, "e.3");
    await vi.advanceTimersByTimeAsync(3000);
    expect(reread).not.toHaveBeenCalled();
    expect(seen).toEqual(["a", "b", "c"]);
  });

  it("tells everyone to read their state again when the daemon could not resume", async () => {
    vi.useFakeTimers();
    const { subscribeEvents, onReconnect } = await loadResuming();
    subscribeEvents("", () => {});
    const reread = vi.fn();
    onReconnect(reread);
    await vi.advanceTimersByTimeAsync(0);
    ResumingES.all[0].hello(false);
    ResumingES.all[0].emit({ type: "a" }, "e.1");
    ResumingES.all[0].onerror?.();
    await vi.advanceTimersByTimeAsync(1000);
    ResumingES.all[1].hello(false); // restarted: a new run of the daemon
    expect(reread).toHaveBeenCalledTimes(1);
  });

  it("takes an older daemon, which sends no hello, to have lost the gap", async () => {
    vi.useFakeTimers();
    const { subscribeEvents, onReconnect } = await loadResuming();
    subscribeEvents("", () => {});
    const reread = vi.fn();
    onReconnect(reread);
    await vi.advanceTimersByTimeAsync(0);
    ResumingES.all[0].onopen?.();
    ResumingES.all[0].onerror?.();
    await vi.advanceTimersByTimeAsync(1000);
    ResumingES.all[1].onopen?.();
    expect(reread).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1600);
    expect(reread).toHaveBeenCalledTimes(1);
  });

  it("opens again a stream that went silent past its pings", async () => {
    vi.useFakeTimers();
    const { subscribeEvents } = await loadResuming();
    subscribeEvents("", () => {});
    await vi.advanceTimersByTimeAsync(0);
    ResumingES.all[0].hello(false);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(ResumingES.all[0].closed).toBe(false); // quiet, not yet dead
    await vi.advanceTimersByTimeAsync(30_000);
    expect(ResumingES.all[0].closed).toBe(true);
    expect(ResumingES.all.length).toBeGreaterThan(1);
  });

  it("starts afresh on another daemon", async () => {
    vi.useFakeTimers();
    const { subscribeEvents, resetConnection } = await loadResuming();
    subscribeEvents("", () => {});
    await vi.advanceTimersByTimeAsync(0);
    ResumingES.all[0].hello(false);
    ResumingES.all[0].emit({ type: "a" }, "e.9");
    resetConnection();
    await vi.advanceTimersByTimeAsync(0);
    expect(ResumingES.all.at(-1)!.url).not.toContain("since=");
  });
});
