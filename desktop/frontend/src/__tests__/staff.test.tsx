import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { StaffMember } from "~/lib/types";

const calls: [string, unknown][] = [];
let members: StaffMember[] = [];
vi.mock("../lib/rpc", () => ({
  rpc: vi.fn(async (method: string, params: unknown) => {
    calls.push([method, params]);
    if (method === "staff.members") return members;
    if (method === "staff.watches") return [{ id: "W1", sessionId: "S9", profileId: "B", instruction: "Testleri gözden geçir", dailyLimit: 10, enabled: true }];
    if (method === "staff.schedules") return [{ id: "K1", profileId: "B", instruction: "Bağımlılıklara bak", dailyAt: "09:00", enabled: false }];
    if (method === "session.list") return [{ id: "S9", title: "Web sitesi", agentId: "a", workspaceId: "w", updatedAt: "" }];
    if (method === "staff.monitors") return [{ id: "M1", profileId: "B", name: "CI hataları", command: "gh run list", instruction: "Düzelt", everyMinutes: 15, enabled: true, lastError: "gh: not logged in" }];
    if (method === "staff.handoffs") return [{ id: "H1", fromId: "A", toId: "B", instruction: "Test yaz", dailyLimit: 10, enabled: true }];
    if (method === "staff.monitorPresets") return [{ key: "gh-ci", name: "GitHub CI failures", command: "gh run list --status failure", instruction: "Fix it", every: 15, needs: "gh" }];
    if (method === "staff.diff") return { patch: "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new" };
    return { ok: true };
  }),
  subscribeEvents: vi.fn(() => () => {}),
}));
vi.mock("../lib/toast", () => ({ toast: vi.fn() }));
vi.mock("../hooks/usePrefs", () => ({ usePrefs: () => ({ lang: "tr", theme: "graphite" }) }));

import { StaffBoard } from "../app/StaffBoard";

const profile = (id: string, name: string, title = "") => ({ id, name, title, role: "developer" as const, systemPrompt: "", model: "", provider: "", isDefault: false, isLeader: false });
const now = new Date().toISOString();

describe("StaffBoard", () => {
  beforeEach(() => {
    calls.length = 0;
    members = [
      {
        profile: profile("A", "Ali", "Backend geliştirici"), state: "working", now: "Login endpoint'i ekle", nowSession: "S1",
        tasks: [{ id: "T1", profileId: "A", sessionId: "S1", title: "Login endpoint'i ekle", brief: "Login endpoint'i ekle", status: "running", seen: false, createdAt: now }],
        notes: [{ id: "N1", key: "go", content: "Kullanıcı Go 1.23 kullanıyor", createdAt: now }],
      },
      {
        profile: profile("B", "Ayşe", "Tasarımcı"), state: "report",
        tasks: [
          { id: "T2", profileId: "B", sessionId: "S2", title: "Ekranı çiz", brief: "Ekranı çiz", status: "done", report: "Çizildi, Figma'da.", seen: false, createdAt: now, endedAt: now },
          { id: "T3", profileId: "B", sessionId: "S3", title: "İkon seç", brief: "İkon seç", from: "A", fromName: "Ali", status: "done", report: "Seçildi", seen: true, createdAt: now, endedAt: now },
        ],
        notes: [],
      },
      { profile: profile("C", "Can"), state: "idle", tasks: [], notes: [] },
    ];
  });
  afterEach(cleanup);

  const card = (id: string) => document.querySelector(`[data-agent="${id}"]`) as HTMLElement;

  // an older daemon sends an empty list as null; the board must not fall over
  it("copes with an agent whose tasks and notes come as null", async () => {
    members = [{ profile: profile("Z", "Zeynep"), state: "idle", tasks: null, notes: null } as unknown as StaffMember];
    render(<StaffBoard workspace={null} onOpenSession={vi.fn()} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    await screen.findByText("Zeynep");
    expect(card("Z").querySelector(".staff-notes-btn")?.textContent).toContain("(0)");
  });

  it("shows each agent like a teammate: title, state, what it is on", async () => {
    const open = vi.fn();
    render(<StaffBoard workspace={null} onOpenSession={open} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    await screen.findByText("Ali");
    expect(card("A").textContent).toContain("Backend geliştirici");
    expect(card("A").querySelector(".staff-chip")?.textContent).toBe("Çalışıyor");
    expect(card("B").querySelector(".staff-chip")?.textContent).toBe("Rapor hazır");
    expect(card("C").querySelector(".staff-chip")?.textContent).toBe("Boşta");
    expect(card("C").textContent).toContain("Unvan yok");
    // the header counts who is working and whose report waits
    expect(document.querySelector(".staff-counts")?.textContent).toContain("1 çalışıyor");
    expect(document.querySelector(".staff-counts")?.textContent).toContain("1 rapor hazır");
    // what it is on opens that conversation
    fireEvent.click(card("A").querySelector(".staff-now")!);
    expect(open).toHaveBeenCalledWith("S1");
    // a colleague's request says who asked
    expect(card("B").querySelector(".staff-from")?.textContent).toBe("← Ali");
  });

  it("hands a task over with Enter, and reading a report marks it read", async () => {
    render(<StaffBoard workspace={{ id: "W", name: "w", path: "/w" } as never} onOpenSession={vi.fn()} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    await screen.findByText("Can");
    const box = card("C").querySelector("textarea")!;
    expect(box.placeholder).toContain("Can için bir iş yaz");
    fireEvent.change(box, { target: { value: "README'yi güncelle" } });
    await act(async () => { fireEvent.keyDown(box, { key: "Enter" }); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.assign")?.[1]).toEqual({ profileId: "C", task: "README'yi güncelle", workspaceId: "W" }));

    const row = card("B").querySelector(".staff-task.unseen .staff-task-row") as HTMLElement;
    await act(async () => { fireEvent.click(row); });
    expect(card("B").querySelector(".staff-report pre")?.textContent).toBe("Çizildi, Figma'da.");
    await waitFor(() => expect(calls.find(([m]) => m === "staff.seen")?.[1]).toEqual({ id: "T2" }));
  });

  it("shows an agent's notes, and they can be added and removed", async () => {
    render(<StaffBoard workspace={null} onOpenSession={vi.fn()} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    await screen.findByText("Ali");
    fireEvent.click(card("A").querySelector(".staff-notes-btn")!);
    expect(card("A").querySelector(".staff-note")?.textContent).toContain("Kullanıcı Go 1.23 kullanıyor");
    await act(async () => { fireEvent.click(card("A").querySelector(".staff-note .tw-helper-x")!); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.noteDelete")?.[1]).toEqual({ id: "N1" }));
    const input = card("A").querySelector(".staff-note-add input") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "Testler go test ./... ile" } });
    await act(async () => { fireEvent.submit(input.closest("form")!); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.noteAdd")?.[1]).toEqual({ profileId: "A", note: "Testler go test ./... ile" }));
  });

  it("lists an agent's automations, each switchable and removable", async () => {
    render(<StaffBoard workspace={null} onOpenSession={vi.fn()} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    // a handoff, a monitor, a watch and a schedule
    await waitFor(() => expect(card("B").querySelectorAll(".staff-auto")).toHaveLength(4));
    const rows = Array.from(card("B").querySelectorAll(".staff-auto")) as HTMLElement[];
    const watch = rows.find((r) => r.textContent?.includes("Web sitesi"))!;
    const sched = rows.find((r) => r.textContent?.includes("Her gün"))!;
    expect(watch.textContent).toContain('"Web sitesi" sohbetini izliyor');
    expect(sched.textContent).toContain("Her gün 09:00");
    expect(sched.className).toContain("off");
    await act(async () => { fireEvent.click(sched.querySelector(".staff-auto-btn")!); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.scheduleSave")?.[1]).toMatchObject({ id: "K1", enabled: true }));
    // removing asks twice
    const x = watch.querySelector(".tw-helper-x") as HTMLElement;
    fireEvent.click(x);
    expect(calls.some(([m]) => m === "staff.watchDelete")).toBe(false);
    await act(async () => { fireEvent.click(x); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.watchDelete")?.[1]).toEqual({ id: "W1" }));
    // the pairing shows on both: Ali hands on, Ayşe takes on
    await waitFor(() => expect(card("A").querySelector(".staff-autos")?.textContent).toContain("Bitirince Ayşe devralır"));
    expect(card("B").querySelector(".staff-autos")?.textContent).toContain("Ali bitirince devralır");
  });

  it("a task's changes wait in its own copy: seen, applied, opened as a PR", async () => {
    members[1].tasks[0] = { ...members[1].tasks[0], isolation: { state: "review", files: ["x.go", "y.go"], added: 12, removed: 3 } };
    render(<StaffBoard workspace={null} onOpenSession={vi.fn()} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    await screen.findByText("Ayşe");
    expect(card("B").querySelector(".staff-review-chip")?.textContent).toBe("inceleme bekliyor");
    await act(async () => { fireEvent.click(card("B").querySelector(".staff-task-row")!); });
    const ch = card("B").querySelector(".staff-changes")!;
    expect(ch.textContent).toContain("2 dosya · +12 −3");
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Değişiklikleri gör" })); });
    await waitFor(() => expect(ch.querySelector(".staff-diff .add")?.textContent).toContain("+new"));
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "PR aç" })); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.pr")?.[1]).toEqual({ id: "T2" }));
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Projeye uygula" })); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.apply")?.[1]).toEqual({ id: "T2" }));
  });

  it("learned notes are marked; a monitor is listed with its error and set up from a preset", async () => {
    members[0].notes.push({ id: "N2", key: "learned:testler go", content: "Testler go test ile", createdAt: now });
    render(<StaffBoard workspace={{ id: "W", name: "w", path: "/w" } as never} onOpenSession={vi.fn()} onTalk={vi.fn()} onEdit={vi.fn()} onAdd={vi.fn()} />);
    await screen.findByText("Ali");
    fireEvent.click(card("A").querySelector(".staff-notes-btn")!);
    const learned = Array.from(card("A").querySelectorAll(".staff-note")).find((n) => n.textContent?.includes("Testler go test ile"))!;
    expect(learned.querySelector(".staff-learned")?.textContent).toBe("öğrendi");
    // Ayşe's monitor, with what went wrong last time
    await waitFor(() => expect(Array.from(card("B").querySelectorAll(".staff-auto")).some((r) => r.textContent?.includes("CI hataları"))).toBe(true));
    expect(card("B").querySelector(".staff-auto-error")?.textContent).toContain("gh: not logged in");
    // set one up for Ali from a preset
    fireEvent.click(Array.from(card("A").querySelectorAll(".staff-foot button")).find((b) => b.textContent?.includes("Gözcü"))!);
    const preset = await waitFor(() => {
      const b = card("A").querySelector(".staff-monitor-presets button") as HTMLElement | null;
      expect(b).toBeTruthy();
      return b!;
    });
    fireEvent.click(preset);
    await act(async () => { fireEvent.submit(card("A").querySelector(".staff-monitor-form")!); });
    await waitFor(() => expect(calls.find(([m]) => m === "staff.monitorSave")?.[1]).toEqual({
      profileId: "A", name: "GitHub CI failures", command: "gh run list --status failure", instruction: "Fix it", everyMinutes: 15, workspaceId: "W",
    }));
  });
});
