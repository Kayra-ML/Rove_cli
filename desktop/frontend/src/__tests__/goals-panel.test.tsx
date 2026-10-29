import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Goal, HarnessCategory, HarnessView, JudgeVerdict } from "../lib/types";

// A stand-in daemon: the goals, their harness and verdicts, and every call.
const calls: [string, any][] = [];
let goals: Goal[] = [];
let views: Record<string, HarnessView> = {};
let verdicts: Record<string, JudgeVerdict[]> = {};
const handlers = new Map<string, ((ev: { type: string; payload?: unknown }) => void)[]>();

const catalog: HarnessCategory[] = [
  { key: "context", tactics: [{ key: "selective", bit: 1 }, { key: "large", bit: 2 }, { key: "fresh", bit: 4 }, { key: "repoMap", bit: 8 }, { key: "memory", bit: 16 }] },
  { key: "execution", tactics: [{ key: "direct", bit: 1 }, { key: "planExecute", bit: 2 }, { key: "goalLoop", bit: 4 }, { key: "sandboxed", bit: 32 }] },
  { key: "tools", tactics: [{ key: "sequential", bit: 1 }, { key: "parallel", bit: 2 }, { key: "restricted", bit: 4 }, { key: "coding", bit: 8 }, { key: "research", bit: 16 }] },
  { key: "verify", tactics: [{ key: "fast", bit: 1 }, { key: "test", bit: 2 }, { key: "independentReview", bit: 4 }, { key: "doubleReview", bit: 8 }, { key: "artifact", bit: 16 }] },
  { key: "recovery", tactics: [{ key: "retry", bit: 1 }, { key: "replan", bit: 2 }, { key: "modelFallback", bit: 4 }, { key: "contextReset", bit: 8 }, { key: "checkpointResume", bit: 16 }, { key: "escalation", bit: 32 }] },
];

const view = (goalId: string, running: boolean): HarnessView => ({
  goalId,
  profile: { mode: "auto", context: 1, execution: 2, tools: 9, verify: 2, recovery: 17, version: 0 },
  explain: "",
  resolved: { maxTurns: 16, contextBudget: 32000, toolConcurrency: 1, allowedTools: ["read_file", "write_file"], tools: "coding tools", verify: "", recovery: ["retry", "checkpoint_resume"], recoveryNote: "", maxRetries: 3 },
  running,
  checkpoint: { goalId, iteration: 1, progress: ["1: wrote the parser → CONTINUE: tests fail"], savedAt: "2026-10-29T10:00:00Z" },
  mutations: [],
});

vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: any) => {
    calls.push([method, params]);
    switch (method) {
      case "goal.list": return goals;
      case "harness.catalog": return catalog;
      case "harness.get": return views[params.goalId];
      case "goal.verdicts": return verdicts[params.id] ?? [];
      case "harness.set": {
        const v = views[params.goalId];
        const next: HarnessView = {
          ...v,
          profile: { ...params.profile, version: v.profile.version + 1 },
          mutations: [...(v.mutations ?? []), { iteration: 1, reason: "set by hand", old: v.profile, new: params.profile, at: "2026-10-29T10:05:00Z" }],
        };
        views[params.goalId] = next;
        return next;
      }
      case "session.create": return { id: "chat-new", title: params.title, agentId: "a", workspaceId: "w1", updatedAt: "" };
      default: return { ok: true };
    }
  }),
  onReconnect: vi.fn(() => () => {}),
  subscribeEvents: vi.fn((filter: string, fn: (ev: { type: string; payload?: unknown }) => void) => {
    handlers.set(filter, [...(handlers.get(filter) ?? []), fn]);
    return () => handlers.set(filter, (handlers.get(filter) ?? []).filter((f) => f !== fn));
  }),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));
vi.mock("../lib/poll", () => ({ pollWhileVisible: (run: () => void) => { run(); return () => {}; } }));

import { GoalsPanel } from "../app/GoalsPanel";

const fire = (filter: string, payload: unknown) =>
  act(() => { (handlers.get(filter) ?? []).forEach((fn) => fn({ type: filter, payload })); });
const rowOf = (title: string) => screen.getByText(title).closest(".goal-row") as HTMLElement;

describe("Settings → Goals", () => {
  beforeEach(() => {
    calls.length = 0;
    handlers.clear();
    goals = [
      { id: "g1", title: "Ödeme servisi", status: "running", iteration: 2, sessionId: "c1", workspaceId: "w1", completionContract: { criteria: ["x"], maxIterations: 8 }, lastVerdict: { id: "v", goalId: "g1", decision: "CONTINUE", reason: "testler kırmızı", iteration: 2, createdAt: "" } },
      { id: "g2", title: "Giriş sayfası", status: "canceled", iteration: 1, sessionId: "c2", workspaceId: "w1", completionContract: { criteria: ["y"], maxIterations: 4 } },
      { id: "g3", title: "README", status: "done", iteration: 1, sessionId: "c3", completionContract: { criteria: ["z"], maxIterations: 8 } },
      { id: "g4", title: "Göç", status: "blocked", iteration: 4, sessionId: "c4", completionContract: { criteria: ["w"], maxIterations: 4 } },
    ];
    views = { g1: view("g1", true), g2: view("g2", false) };
    verdicts = { g2: [{ id: "v1", goalId: "g2", decision: "CONTINUE", reason: "eksik: form", iteration: 1, createdAt: "", gateResults: [{ name: "check", passed: false, exitCode: 1, output: "" }] }] };
  });
  afterEach(cleanup);

  it("shows where each goal stands, and runs, stops and resumes them", async () => {
    render(<GoalsPanel workspace={{ id: "w9", name: "other", path: "/other", defaultBranch: "main" }} sessionId="open-chat" />);
    await screen.findByText("Ödeme servisi");
    const running = rowOf("Ödeme servisi");
    expect(running.textContent).toContain("Çalışıyor");
    expect(running.textContent).toContain("tur 2/8");
    expect(running.textContent).toContain("testler kırmızı");
    // a finished goal is not run again, nor one that used all its iterations
    expect(within(rowOf("README")).queryByRole("button", { name: /Çalıştır|Sürdür/ })).toBeNull();
    expect(rowOf("Göç").textContent).toContain("tur 4/4");
    expect(within(rowOf("Göç")).queryByRole("button", { name: /Çalıştır|Sürdür/ })).toBeNull();

    fireEvent.click(within(running).getByRole("button", { name: /Durdur/ }));
    await waitFor(() => expect(calls).toContainEqual(["goal.cancel", { id: "g1" }]));

    // a stopped goal resumes in its own chat and its own workspace
    fireEvent.click(within(rowOf("Giriş sayfası")).getByRole("button", { name: /Sürdür/ }));
    await waitFor(() => expect(calls).toContainEqual(["goal.drive", { id: "g2", sessionId: "c2", workspace: "" }]));
  });

  it("opens a goal's harness: its tactics, picking one by hand, and the trace", async () => {
    render(<GoalsPanel />);
    await screen.findByText("Giriş sayfası");
    fireEvent.click(within(rowOf("Giriş sayfası")).getByRole("button", { name: /Ayrıntılar/ }));
    await screen.findByText("Çalışma profili");
    expect(screen.getByRole("button", { name: "Seçici" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Depo haritası" }).getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByText(/turda en fazla 16 adım/).textContent).toContain("32k token bağlam");
    expect(screen.getByText("Yeniden dene → Kaldığı yerden")).toBeTruthy();
    expect(screen.getByText(/wrote the parser/)).toBeTruthy();
    // the reviewer's verdict on the iteration, with the check that failed
    expect(screen.getByText("eksik: form")).toBeTruthy();
    expect(screen.getByText("✗ check")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Depo haritası" }));
    await waitFor(() => expect(calls.some(([m]) => m === "harness.set")).toBe(true));
    const [, sent] = calls.find(([m]) => m === "harness.set")!;
    expect(sent.goalId).toBe("g2");
    expect(sent.profile.context).toBe(1 | 8);
    expect(sent.profile.mode).toBe("manual");
    await screen.findByText("elle seçildi");
    expect(screen.getByText("+Depo haritası")).toBeTruthy();
    expect(screen.getByText(/Elle · v1/)).toBeTruthy();
  });

  it("keeps a running goal's harness as it is", async () => {
    render(<GoalsPanel />);
    await screen.findByText("Ödeme servisi");
    fireEvent.click(within(rowOf("Ödeme servisi")).getByRole("button", { name: /Ayrıntılar/ }));
    await screen.findByText("Değiştirmek için hedefi durdur");
    expect((screen.getByRole("button", { name: "Seçici" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Otomatik seçtir" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("follows the daemon: a goal's events read the list again", async () => {
    render(<GoalsPanel />);
    await screen.findByText("Ödeme servisi");
    const before = calls.filter(([m]) => m === "goal.list").length;
    goals = goals.map((g) => (g.id === "g1" ? { ...g, status: "done", iteration: 3 } : g));
    fire("goal.updated", { id: "g1", status: "done" });
    await waitFor(() => expect(calls.filter(([m]) => m === "goal.list").length).toBeGreaterThan(before));
    await waitFor(() => expect(rowOf("Ödeme servisi").textContent).toContain("Bitti"));
  });

  it("makes a goal in a chat of its own, its check a quality gate", async () => {
    render(<GoalsPanel workspace={{ id: "w1", name: "p", path: "/p", defaultBranch: "main" }} />);
    await screen.findByText("Ödeme servisi");
    fireEvent.change(screen.getByLabelText("Başlık"), { target: { value: "Arama" } });
    fireEvent.change(screen.getByLabelText("Kriterler"), { target: { value: "hızlı, doğru" } });
    fireEvent.change(screen.getByLabelText("En fazla tur"), { target: { value: "5" } });
    fireEvent.change(screen.getByLabelText("Kontrol komutu"), { target: { value: "go test ./..." } });
    fireEvent.click(screen.getByRole("button", { name: "Oluştur" }));
    await waitFor(() => expect(calls.some(([m]) => m === "goal.create")).toBe(true));
    expect(calls.find(([m]) => m === "session.create")![1]).toMatchObject({ title: "Arama", space: "chat", mode: "orchestra", workspaceId: "w1" });
    expect(calls.find(([m]) => m === "goal.create")![1]).toEqual({
      title: "Arama",
      sessionId: "chat-new",
      workspaceId: "w1",
      completionContract: {
        criteria: ["hızlı", "doğru"],
        qualityGates: [{ name: "check", kind: "custom", command: "go test ./...", expectExitZero: true }],
        maxIterations: 5,
      },
    });
  });

  it("asks once before deleting", async () => {
    render(<GoalsPanel />);
    await screen.findByText("README");
    const del = within(rowOf("README")).getByRole("button", { name: "Sil" });
    fireEvent.click(del);
    expect(del.textContent).toBe("Silinsin mi?");
    expect(calls.some(([m]) => m === "goal.delete")).toBe(false);
    fireEvent.click(del);
    await waitFor(() => expect(calls).toContainEqual(["goal.delete", { id: "g3" }]));
  });
});
