# ADR 0001 — Single Go Core, dual clients

## Status

Accepted

## Context

CLI and Desktop must not fork the agent system. Long-running terminals, kanban jobs and goals have to survive a crashed UI. Future remote workers should not force a rewrite.

## Decision

1. **Go Core** owns agents, sessions, goals, tools, terminals, providers, skills, kanban, workspaces, git, MCP, memory and persistence.
2. **`aetherd`** is a persistent process. It serves authenticated JSON-RPC over a Unix socket (Windows: named pipe path in config) and loopback HTTP + SSE for events.
3. **CLI and Desktop** are RPC clients. Wails bindings only forward `RPC(method, params)` — no domain logic in React.
4. **SQLite WAL** stores structured state. Secrets use AES-GCM with a master key in a 0600 file (OS keyring interface ready).
5. **xterm.js** renders bytes. Process lifetime stays in Go (PTY on unix, process pipes + ConPTY-shaped API on Windows).
6. **Judge + quality gates** decide goal completion. Agent text is evidence, not truth.
7. **Worktrees + file leases** isolate parallel agents.
8. **`internal/worker.Worker`** is the remote-worker seam. Today the pool always returns the local runtime.

## Consequences

- A UI crash cannot corrupt a running card: cards in `running` are moved back to `ready` on daemon restart.
- Adding a remote worker means implementing `Worker` and registering it on the pool; Goal/Kanban/Orchestrator stay put.
- Desktop without WebKitGTK can still be used via the CLI and a browser pointed at `http://127.0.0.1:7420` (token required).
