import type { Column } from "./types";

export function columnLabel(c: Column): string {
  switch (c) {
    case "backlog":
      return "Backlog";
    case "ready":
      return "Ready";
    case "running":
      return "Running";
    case "review":
      return "Review";
    case "done":
      return "Done";
    case "blocked":
      return "Blocked";
  }
}

export function isPrimaryWorkspace(view: string): view is "chat" | "kanban" {
  return view === "chat" || view === "kanban";
}
