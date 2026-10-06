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
