# ADR 0002 — Goal completion is judged, not claimed

## Status

Accepted

## Context

Autonomous goals must not stop because the implementation agent says they are finished.

## Decision

A goal carries a **completion contract**: criteria, optional deterministic quality gates (build/test/lint/custom commands), and a max iteration cap.

After each agent turn the **judge**:

- runs quality gates in the worktree
- checks criteria against artifacts / evidence
- returns `DONE`, `CONTINUE`, or `BLOCKED`

An agent claim of completion while gates or criteria fail is forced to `CONTINUE` with an explicit reason.

## Consequences

Goals can loop without a human in the seat. Max iterations prevent runaway spend. Kanban cards in goal mode move to Review on DONE and Blocked on BLOCKED.
