import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { cableState, graphOf, linksOf, startLabel, statsOf, wavesOf } from "~/lib/teamwork";
import type { Session, WorkPlan } from "~/lib/types";

const phase = (id: string, wave: number, extra: Partial<WorkPlan["phases"][number]> = {}) => ({
  id, title: `${id} title`, goal: `${id} goal`, files: [`src/${id}`], agents: [{ character: "frontend" }], wave, ...extra,
});
const plan = (over: Partial<WorkPlan> = {}): WorkPlan => ({
  id: "P", sessionId: "S", prompt: "Google login", summary: "Google login", status: "draft", createdAt: "", updatedAt: "",
  phases: [
    phase("F1", 1, { agents: [{ character: "go-backend" }, { character: "qa", why: "edge cases" }], files: ["src/server/auth", "src/lib/auth", "src/x"] }),
    phase("F2", 1),
    phase("F3", 2, { dependsOn: ["F1"] }),
    { id: "M", title: "Merge", goal: "merge", files: [], agents: [{ character: "reviewer" }], wave: 3, merge: true, dependsOn: ["F1", "F2", "F3"] },
  ],
  notes: [{ kind: "merged", phases: ["F2", "F4"], detail: "src/auth" }],
  ...over,
});

describe("teamwork helpers", () => {
  it("groups waves, counts, and labels starts", () => {
    const p = plan();
    const { waves, merge } = wavesOf(p);
    expect(waves.map((w) => [w.n, w.phases.map((x) => x.id)])).toEqual([[1, ["F1", "F2"]], [2, ["F3"]]]);
    expect(merge?.id).toBe("M");
    expect(statsOf(p)).toEqual({ phases: 3, agents: 5, parallel: 2, junctions: 1 });
    expect(startLabel(p.phases[0], p).kind).toBe("parallel");
    expect(startLabel(p.phases[2], p)).toEqual({ kind: "after", after: ["F1"] });
  });
  it("links dependencies, and phases nothing waits on into the merge", () => {
    expect(linksOf(plan())).toEqual([["F1", "F3"], ["F2", "M"], ["F3", "M"]]);
  });
  it("lays orchestras out in columns, cables between them, a junction where cables meet", () => {
    const g = graphOf(plan());
    const x = (id: string) => g.nodes.get(id)!.x;
    expect(x("F1")).toBe(x("F2"));
    expect(x("F3")).toBeGreaterThan(x("F1"));
    expect(x("M")).toBeGreaterThan(x("F3"));
    // F2 and F3 both feed the merge: they meet at one junction in front of it
    expect(g.junctions).toHaveLength(1);
    const j = g.junctions[0];
    expect(j.to).toBe("M");
    expect([...j.from].sort()).toEqual(["F2", "F3"]);
    expect(j.x).toBeLessThan(x("M"));
    expect(j.x).toBeGreaterThan(x("F3") + g.nodes.get("F3")!.w);
    // F1→F3 is a plain cable; F2, F3 → junction; junction → M
    expect(g.cables.map((c) => `${c.from}>${c.into}`).sort()).toEqual(["F1>card", "F2>junction", "F3>junction", "j-M>out"]);
    expect(g.width).toBeGreaterThan(x("M"));
  });
  it("a cable shows whether the work has gone through it", () => {
    const p = plan({ phases: plan().phases.map((x) => ({ ...x, status: x.id === "F2" ? "done" as const : x.id === "F3" ? "running" as const : "pending" as const })) });
    const g = graphOf(p);
    const j = g.junctions[0];
    const by = (k: string) => g.cables.find((c) => c.key.startsWith(k))!;
    expect(cableState(p, by("F2>"))).toBe("done");
    expect(cableState(p, by("F3>"))).toBe("running");
    expect(cableState(p, by("j-M>"), j)).toBe("running");
    expect(cableState(p, by("F1>"))).toBe("idle");
  });
});

// --- the view, against a mocked daemon ---
const calls: [string, unknown][] = [];
let current: WorkPlan | null = null;
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    switch (method) {
      case "teamwork.get": return current;
      case "teamwork.plan": {
        const q = params as { model?: string; provider?: string };
        current = { ...plan(), planner: q.model ? { provider: q.provider ?? "", model: q.model } : { provider: "home", model: "small" } };
        return current;
      }
      case "session.model": return { provider: "home", model: "small", source: "agent" };
      case "model.list": return [{ provider: "big", model: "planner-x" }, { provider: "home", model: "small", default: true }];
      case "teamwork.approve": current = { ...plan(), status: "running" }; return current;
      case "teamwork.removePhase": current = { ...plan(), phases: plan().phases.filter((p) => p.id !== (params as { phase: string }).phase) }; return current;
      case "persona.catalog": return { characters: [{ id: "frontend", name: "Frontend Uzmanı", category: "Yazılım", summary: "", role: "frontend", features: [], prompt: "" }], features: [], defaults: [] };
      default: return null;
    }
  }),
  subscribeEvents: vi.fn(() => () => {}),
  hydrateConnection: vi.fn().mockResolvedValue({ token: "t", http: "http://x" }),
  pickFolder: vi.fn(),
}));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { TeamworkView } from "../app/TeamworkView";

const sess: Session = { id: "S", title: "t", agentId: "a", workspaceId: "w", updatedAt: "" };

describe("TeamworkView", () => {
  beforeEach(() => { calls.length = 0; current = null; });
  afterEach(cleanup);

  it("asks for a job, plans it (creating the chat if needed), and shows cards — not code", async () => {
    const create = vi.fn(async () => sess);
    render(<TeamworkView session={null} draft onCreateSession={create} workspace={null} />);
    fireEvent.change(await screen.findByPlaceholderText(/Google ile oturum/), { target: { value: "Google login ekle" } });
    fireEvent.click(screen.getByRole("button", { name: "Plan çıkar" }));
    await waitFor(() => expect(calls.some(([m]) => m === "teamwork.plan")).toBe(true));
    expect(create).toHaveBeenCalled();
    expect(calls.find(([m]) => m === "teamwork.plan")?.[1]).toEqual({ sessionId: "S", prompt: "Google login ekle" });
    await screen.findByText("3 orkestra · 5 ajan");
    expect(document.querySelectorAll(".tw-card")).toHaveLength(4);
    expect(document.querySelector(".tw-card.merge")).toBeTruthy();
    expect(screen.getByText("2 paralel → Toparlama")).toBeTruthy();
    expect(screen.getByText("F2, F4 birleştirildi: ikisi de src/auth dosyalarına dokunuyor")).toBeTruthy();
    // each card is an orchestra: its conductor, then its members (what they do on hover)
    const f1 = document.querySelector('[data-phase="F1"]') as HTMLElement;
    expect(f1.querySelector(".tw-player.lead")?.textContent).toBe("Şefgo-backend");
    expect(Array.from(f1.querySelectorAll(".tw-player:not(.lead)")).map((c) => [c.textContent, c.getAttribute("title")])).toEqual([["qa×", "edge cases"]]);
    // a port on each side for its cables, and a status line
    expect(f1.querySelectorAll(".tw-port")).toHaveLength(2);
    expect(f1.querySelector(".tw-card-foot")?.textContent).toBe("Onay bekliyor");
    expect((document.querySelector('[data-phase="F2"]') as HTMLElement).textContent).toContain("tek başına çalar");
    // the merge's two inputs meet at a junction
    expect(document.querySelectorAll(".tw-junction")).toHaveLength(1);
    expect(document.querySelector("pre code, .markdown")).toBeNull();
  });

  it("the planner's model is picked next to the box, shown on the plan, and used to re-plan", async () => {
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    // by default the chat's model plans
    const pill = await screen.findByRole("button", { name: "Planı yazan model" });
    await waitFor(() => expect(pill.textContent).toContain("small"));
    // it sits in a bar at the panel's top, not in the request box, which
    // keeps its whole width for the request
    expect(pill.closest(".tw-panel > .tw-planner-bar")).toBeTruthy();
    expect(document.querySelector(".tw-dock .model-pill")).toBeNull();
    fireEvent.click(pill);
    const menu = await screen.findByRole("listbox", { name: "Planı yazan model" });
    expect(Array.from(menu.querySelectorAll("[role=option] code")).map((c) => c.textContent)).toEqual(["↺ Sohbetin modeli", "planner-x", "small"]);
    fireEvent.click(screen.getByRole("option", { name: /planner-x/ }));
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(pill.textContent).toContain("planner-x");

    fireEvent.change(screen.getByPlaceholderText(/Google ile oturum/), { target: { value: "Google login ekle" } });
    fireEvent.click(screen.getByRole("button", { name: "Plan çıkar" }));
    await waitFor(() => expect(calls.filter(([m]) => m === "teamwork.plan")[0]?.[1]).toEqual({ sessionId: "S", prompt: "Google login ekle", provider: "big", model: "planner-x" }));
    // the plan says who wrote it
    expect((await screen.findByTitle("Planlayan: big/planner-x")).textContent).toContain("planner-x");

    // re-plan the draft with the same model, without retyping it
    fireEvent.click(screen.getByRole("button", { name: "Yeniden planla" }));
    await waitFor(() => expect(calls.filter(([m]) => m === "teamwork.plan")[1]?.[1]).toEqual({ sessionId: "S", prompt: "Google login", provider: "big", model: "planner-x" }));

    // back to the chat's model: no model is sent
    fireEvent.click(pill);
    fireEvent.click(await screen.findByRole("option", { name: /Sohbetin modeli/ }));
    await waitFor(() => expect(pill.textContent).toContain("small"));
    fireEvent.click(screen.getByRole("button", { name: "Yeniden planla" }));
    await waitFor(() => expect(calls.filter(([m]) => m === "teamwork.plan")[2]?.[1]).toEqual({ sessionId: "S", prompt: "Google login" }));
  });

  it("the request box is glass until there is something in it", async () => {
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    const box = await screen.findByPlaceholderText(/Google ile oturum/);
    const dock = () => document.querySelector(".composer-dock")!;
    expect(dock().className).not.toContain("filled");
    fireEvent.change(box, { target: { value: "bir iş" } });
    expect(dock().className).toContain("filled");
  });

  it("nothing runs before approval; removing a phase re-plans the draft", async () => {
    current = plan();
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    await screen.findByText("3 orkestra · 5 ajan");
    expect(calls.some(([m]) => m === "teamwork.approve")).toBe(false);
    fireEvent.click((document.querySelector('[data-phase="F2"]') as HTMLElement).querySelector(".tw-x")!);
    await waitFor(() => expect(calls.find(([m]) => m === "teamwork.removePhase")?.[1]).toEqual({ planId: "P", phase: "F2" }));
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Onayla ve başlat" })); });
    await waitFor(() => expect(calls.find(([m]) => m === "teamwork.approve")?.[1]).toEqual({ planId: "P" }));
    await screen.findByText("Çalışıyor");
    // once started, cards can't be edited
    expect(document.querySelector(".tw-x")).toBeNull();
  });

  it("orchestras on the left, the conversation and its box in the panel on the right", async () => {
    current = plan();
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    await screen.findByText("3 orkestra · 5 ajan");
    expect(document.querySelector(".tw > .tw-stage .tw-board .tw-graph")).toBeTruthy();
    const panel = document.querySelector(".tw > aside.tw-panel") as HTMLElement;
    // columns are labelled by when they start, the merge last
    expect(Array.from(document.querySelectorAll(".tw-col-label")).map((c) => c.textContent)).toEqual(["1. dalga · hemen başlar", "2. dalga", "Toparlama"]);
    // the request is the thread's first message, the plan answers it
    expect(panel.querySelector(".tw-bubble")?.textContent).toBe("Google login");
    const dock = panel.querySelector(".composer-dock") as HTMLElement;
    expect(dock).toBeTruthy();
    // the draft's approval lives right above the box
    expect(dock.textContent).toContain("Plan hazır");
    expect(dock.querySelector("button.primary")?.textContent).toBe("Onayla ve başlat");
    // Enter sends a new request (it replaces the draft); Shift+Enter does not
    const box = dock.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "başka bir iş" } });
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
    expect(calls.some(([m]) => m === "teamwork.plan")).toBe(false);
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(calls.find(([m]) => m === "teamwork.plan")?.[1]).toEqual({ sessionId: "S", prompt: "başka bir iş" }));
    await waitFor(() => expect(box.value).toBe(""));
  });

  it("while running: the box is locked, Stop is above it, and a card opens its orchestra in the panel", async () => {
    current = { ...plan(), status: "running", phases: plan().phases.map((p) => ({ ...p, status: p.id === "F1" ? "running" as const : "pending" as const })) };
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    await screen.findByText("3 orkestra · 5 ajan");
    const dock = document.querySelector(".tw-panel .composer-dock") as HTMLElement;
    expect((dock.querySelector("textarea") as HTMLTextAreaElement).disabled).toBe(true);
    expect(dock.textContent).toContain("Çalışıyor · 0/4");
    expect(Array.from(dock.querySelectorAll("button")).some((b) => b.textContent === "Durdur")).toBe(true);
    fireEvent.click(document.querySelector('[data-phase="F1"]') as HTMLElement);
    const orch = await screen.findByRole("region", { name: "F1 title" });
    expect(orch.closest(".tw-panel")).toBeTruthy();
    expect(orch.textContent).toContain("F1 goal");
    expect(document.querySelector('[data-phase="F1"]')?.className).toContain("open");
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("region", { name: "F1 title" })).toBeNull());
    // back on the plan, with its box
    expect(document.querySelector(".tw-panel .composer-dock")).toBeTruthy();
  });

  it("a junction opens the chats of every orchestra that meets there", async () => {
    const ch = (id: string) => ({ character: "frontend", channelId: id });
    current = {
      ...plan(), status: "running",
      phases: plan().phases.map((p) => ({ ...p, status: "running" as const, agents: p.id === "F3" ? [ch("c3"), { ...ch("m3"), character: "qa", why: "tests" }] : [ch(`c-${p.id}`)] })),
    };
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    await screen.findByText("3 orkestra · 5 ajan");
    fireEvent.click(document.querySelector(".tw-junction") as HTMLElement);
    const sheet = await screen.findByRole("region", { name: "Birleşim" });
    expect(document.querySelector(".tw-junction")?.className).toContain("open");
    // the orchestras meeting here, and the one they flow into
    const rows = Array.from(sheet.querySelectorAll(".tw-rail-row")).map((r) => r.querySelector(".tw-id")?.textContent);
    expect(rows.slice(0, 2).sort()).toEqual(["F2", "F3"]);
    expect(sheet.querySelectorAll(".tw-rail-row")).toHaveLength(3);
    expect(sheet.querySelector(".tw-rail-row:last-child")?.textContent).toContain("buraya akar");
    // picking F3 shows its players in the corner pill: the conductor and its member, each a chat
    fireEvent.click(Array.from(sheet.querySelectorAll(".tw-rail-row")).find((r) => r.textContent?.includes("F3"))!);
    fireEvent.click(screen.getByRole("button", { name: "Kimin sohbeti" }));
    const opts = Array.from((await screen.findByRole("listbox", { name: "Orkestra sohbetleri" })).querySelectorAll("[role=option]"));
    expect(opts).toHaveLength(2);
    expect(opts[0].textContent).toContain("Şef");
    fireEvent.click(opts[1]);
    expect(screen.queryByRole("listbox", { name: "Orkestra sohbetleri" })).toBeNull();
    await waitFor(() => expect(sheet.textContent).toContain("tests"));
  });

  it("a stopped plan continues where it stopped", async () => {
    current = { ...plan(), status: "failed", phases: plan().phases.map((p) => ({ ...p, status: p.id === "F1" ? "done" as const : p.id === "F2" ? "failed" as const : "canceled" as const })) };
    render(<TeamworkView session={sess} draft={false} onCreateSession={vi.fn()} workspace={null} />);
    await screen.findByText("3 orkestra · 5 ajan");
    const dock = document.querySelector(".tw-panel .composer-dock") as HTMLElement;
    expect(dock.textContent).toContain("1/4 bitti, korunuyor");
    fireEvent.click(screen.getByRole("button", { name: "Kaldığı yerden devam et" }));
    await waitFor(() => expect(calls.find(([m]) => m === "teamwork.retry")?.[1]).toEqual({ planId: "P" }));
  });
});
