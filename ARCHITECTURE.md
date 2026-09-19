# Rove Code — Architecture & Implementation Notes

## Overview

Rove Code is a local-first coding agent. Its **Go Core** is the single
source of truth for all subsystems. The terminal (`rovecode`), desktop app,
and headless daemon share the same core.

```
┌──────────────────────────────────────────────────────────────┐
│                        Desktop (Wails)                       │
│              React + TypeScript presentation layer           │
│  (Chat, Kanban, Terminal, Settings, Workspace, Files, SSE)   │
└──────────────────────────┬───────────────────────────────────┘
                           │ HTTP JSON-RPC + SSE
┌──────────────────────────▼───────────────────────────────────┐
│                     rovecode daemon                          │
│   HTTP :7420  +  Unix socket (IPC)  –  both auth-gated       │
└──────────────────────────┬───────────────────────────────────┘
                           │ in-process
┌──────────────────────────▼───────────────────────────────────┐
│                       Go Core                                │
│                                                              │
│  Agent Runtime      Goal Engine       Judge Engine           │
│  Multi-Agent Orch   Kanban Engine     Skill Runtime          │
│  Skill Marketplace  Provider Router   Tool Runtime           │
│  MCP Runtime        Terminal Engine   SSH Manager            │
│  Git/Worktree Mgr   Workspace Mgr     Session Manager        │
│  Memory System      Event Bus         Persistent Storage     │
│  Secrets Manager    Permission System  Worker Pool           │
└──────────────────────────────────────────────────────────────┘
                           │
                        rovecode CLI
```

## Key Architectural Decisions

### 1. Single Daemon Owns All State
`rovecode daemon` starts, opens the Go Core (`core.Open`), and serves two concurrent transports:
- **HTTP** (`/rpc` POST, `/events` SSE, `/health` GET)
- **Unix socket** (IPC, newline-delimited JSON)

Both use the same `rpc.Server.Dispatch` handler. The daemon writes a `rovecode.sock`
file (legacy `aether.sock` is still accepted); the CLI prefers IPC, falls back to HTTP.

### 2. No Logic in Desktop / CLI
The desktop `App` struct in Wails is a thin bridge. Its only method is `RPC(method,
paramsJSON) string` which calls `rpc.Server.Dispatch` directly (in-process for desktop,
or via HTTP for headless CLI). Domain logic never crosses into React or CLI packages.

### 3. Goal Engine is Persistent and Autonomous
Goals survive daemon restarts (stored in SQLite). The Judge Engine evaluates evidence
after each agent iteration:
- Runs deterministic quality gates (build, test, lint) using `qualitygate.Runner`
- Returns `DONE | CONTINUE | BLOCKED`
- `CONTINUE` increments the iteration counter and loops back to the agent
- Iteration > `MaxIterations` → `BLOCKED` (not ≥, so max=1 still allows one run)

### 4. Kanban Cards Are Executable
Each Kanban card can be dispatched to an agent. The Orchestrator:
1. Acquires a lease (prevents double-dispatch)
2. Creates/reuses a session
3. Moves the card to `Running`
4. Calls agent.Run
5. On success → `Review` (or `Done` for auto-approve goals)
6. On failure → `Blocked` with log entry

Card dependencies are enforced: a card cannot move to `Ready` if any dependency is
still in `Backlog/Running/Review/Blocked`.

### 5. Terminal Architecture (Orca-style)
The **Go Core owns the process**. `terminal.Engine` spawns PTY processes (Linux/macOS)
or ConPTY stubs (Windows), stores output in a ring buffer, and publishes events on the
bus. The desktop uses `xterm.js` only as a renderer — it calls `terminal.attach` to
get the replay buffer, then streams writes/resizes via RPC.

SSH sessions go through `ssh.Manager → terminal.Engine` with the same API.

### 6. Worker Pool for Future Remote Workers
`worker.Pool` wraps the local agent runtime. Future remote workers implement `worker.Worker`
and call `Pool.Register`. The orchestrator calls `Pool.For(card)` — today it always
returns local; extending to remote requires no changes to Kanban or Goal logic.

### 7. Secrets Manager
API keys are never stored as plaintext in the SQLite database. `secrets.Store` uses
OS keychain facilities (macOS Keychain, Linux SecretService/file+mode-600 fallback,
Windows DPAPI). Provider configs in the DB store only the secret ID.

### 8. Skill Marketplace
Skills are YAML manifests + entrypoints installed under `~/.local/share/aether/skills/`.
Manifests declare permissions (filesystem, shell, network, browser, git). The local
`marketplace.Catalog` mirrors skill metadata as JSON; a future public registry replaces
the catalog backend without changing the skill runtime or the frontend Skill Market page.

### 9. Event Bus
`eventbus.Bus` is a pub/sub bus with string-prefix topic filtering. Subscribers receive
`types.Event` values. The SSE endpoint in the HTTP server subscribes and streams
`data: <json>\n\n` frames. The React frontend uses `EventSource` for live updates
(streaming agent output, terminal data, Kanban changes).

### 10. MCP (Model Context Protocol)
`mcp.Runtime` exposes Go Core tools (read_file, write_file, shell, etc.) as MCP tool
definitions over JSON-RPC. External MCP servers can be registered and their tools
surfaced to agents through the Tool Runtime.

## Package Layout

```
cmd/
  aether/        CLI entry point
  aetherd/       Legacy daemon entry (still builds; product path is `rovecode daemon`)
internal/
  agent/         Agent runtime, RunRequest, RunResult
  clock/         Testable clock wrapper
  config/        Config loading, platform paths
  core/          App struct — wires all subsystems together
  daemon/        HTTP+IPC server lifecycle, PID file
  eventbus/      Pub/sub event bus
  gitwt/         Git + worktree operations
  goal/          Goal engine — persistent autonomous goals
  id/            Compact random ID generation
  judge/         Judge engine — DONE/CONTINUE/BLOCKED decisions
  kanban/        Kanban engine — card CRUD, column moves, dependency checks
  lease/         Distributed lease coordinator (anti-double-dispatch)
  marketplace/   Local skill catalog; publishable to future registry
  mcp/           MCP runtime and JSON-RPC server
  memory/        Memory system — scoped key/content pairs
  orchestrator/  Multi-agent card dispatch, goal-card wiring
  permission/    Rule-based permission engine (allow/deny/ask)
  provider/      Provider router, OpenAI-compat adapter, fake provider
  qualitygate/   Deterministic quality gates (build/test/lint/custom)
  rpc/           JSON-RPC dispatcher, HTTP+IPC server
  secrets/       OS-keychain secrets store
  session/       Session CRUD and message history
  skill/         Skill runtime — manifest loading, install, slash commands
  ssh/           SSH session manager
  store/         SQLite persistence (WAL mode)
  terminal/      PTY/ConPTY process engine, ring buffer, attach/detach
  tool/          Tool runtime — read_file, write_file, shell, git tools
  types/         Shared domain types
  worker/        Worker interface and pool
  workspace/     Workspace open/list, Git branch tracking
pkg/
  client/        Go HTTP+IPC client for CLI and tests
  protocol/      JSON-RPC protocol constants and types
desktop/
  main.go        Wails entry point
  app.go         Wails bridge (RPC proxy only — no agent logic)
  frontend/      React + TypeScript UI
    src/
      app/       Page components (Chat, Kanban, Terminal, Settings, Workspace)
      hooks/     React hooks over RPC layer
      lib/       rpc.ts bridge, types.ts, columns.ts
      styles/    global.css (CSS variables, layout, components)
```

## Running

```sh
# Build everything
go build ./...

# Run the daemon (stays running in background)
rovecode daemon

# CLI
./aether ping
./aether session new "my session"
./aether chat <sessionId> "hello world"
./aether card list
./aether goal create "implement feature X"

# Desktop (requires Wails installed)
cd desktop && wails dev

# Tests
go test ./...
cd desktop/frontend && npx vitest run
```

## Quality Gates Passed (build gate)

All acceptance criteria met. Final gate run (2026-09-14):

| Gate | Status |
|---|---|
| `go build ./cmd/sextant ./cmd/aether ./cmd/aetherd` | ✅ |
| `go vet ./...` | ✅ |
| `go test ./... -count=1` (29 packages, 50+ tests) | ✅ |
| `tsc --noEmit` | ✅ 0 errors |
| `vitest run` | ✅ 5/5 |

### 11. OpenAI-Compatible Provider (real HTTP streaming)

`internal/provider` contains `OpenAICompat` — a full SSE streaming HTTP client against any OpenAI-compatible endpoint (OpenAI, Together, Groq, Anthropic compat layer, etc.). The router selects it by name; the `Fake` provider is always registered for tests. API keys are never stored in plaintext — they are looked up at call time via the `KeyLookup` function which reads from the Secrets Manager.

### 12. Extended RPC Coverage

New RPC methods added to server and protocol:
- `card.logs` — returns a card's structured log entries
- `card.addArtifact` — appends an artifact (file/URL/etc) to a card and persists
- `agent.delete` — removes an agent from the store
- `git.commit` — stage-all + commit in a workspace path
- `git.log` — returns N most recent commits with hash/author/date/message
- `shutdown` — graceful daemon stop (100ms delay, os.Exit)

### 13. Git CommitAll + Log

`internal/gitwt` extended with:
- `CommitAll(path, message)` — runs `git add -A && git commit -m`
- `Log(path, limit)` — parses `git log --pretty=format:...` into `[]GitCommit`

### 14. Artifact + LogEntry Types Aligned

`types.Artifact` now has `Name`, `Label`, `Path`, `URL`, `Kind` fields — matching both the SQLite store and the TypeScript frontend. `types.LogEntry` has `At`, `Level`, `Message`, `Source`.

## Frontend Pages (desktop/frontend/src/app/)

| Page | Description |
|---|---|
| `App.tsx` | Root shell: topbar nav, Chat↔Kanban mode switch, status bar, CardDetail modal |
| `Chat.tsx` | SSE-streaming chat with session history |
| `Kanban.tsx` | 6-column board (Backlog→Done+Blocked), card dispatch/approve/reject |
| `Terminal.tsx` | xterm.js renderer, tabbed sessions, spawn/select |
| `AgentManager.tsx` | Create/edit/delete agents; provider + model selection; system prompt |
| `SkillMarket.tsx` | Installed skills list; local path install; marketplace search + install; permission badges |
| `CardDetail.tsx` | Card modal: acceptance criteria, git branch, commit form, git log, artifacts, logs |
| `Settings.tsx` | Provider upsert, goal create, secret management |
| `Workspace.tsx` | File browser + workspace selector |
| `HarnessPanel.tsx` | Harness profile viewer/editor: mode badge, tactic chips, mutation timeline, 5s live poll, Manual/Auto toggle |

### 15. Execution Policy Matrix — Harness System

`internal/harness/` implements the central execution policy system. Every Goal and Kanban Card carries a `GoalExecProfile` that determines how the engine thinks, what context it sees, how it executes, which tools it uses, how it verifies, and how it recovers.

#### Tactic Types (bitfield composition)

| Type | Flags |
|---|---|
| `ContextTactic` | `SelectiveContext`, `LargeContext`, `FreshContext`, `RepositoryMap`, `MemoryHeavy` |
| `ExecutionTactic` | `Direct`, `PlanExecute`, `GoalLoop`, `Parallel`, `Sandboxed`, `Delegated` |
| `ToolTactic` | `SequentialTools`, `ParallelTools`, `RestrictedTools`, `CodingTools`, `ResearchTools` |
| `VerifyTactic` | `FastVerify`, `TestVerify`, `IndependentReview`, `ArtifactVerify`, `DoubleReview` |
| `RecoveryTactic` | `Retry`, `Replan`, `ModelFallback`, `ContextReset`, `CheckpointResume` |

Multiple tactics per category are active simultaneously (bitwise OR). No giant switch tree — each subsystem reads the flags it cares about.

#### Files

| File | Responsibility |
|---|---|
| `tactics.go` | Bitmask types, `GoalExecProfile`, `Has*`/`Add*` helpers, `Explain()`, preset profiles |
| `composer.go` | `HarnessComposer.Compose(TaskAnalysis)` — scoring-based auto profile selection |
| `context_strategy.go` | `ContextEngine.Resolve()` → `RunContext{Files, Budget, RepoMap, HandoffRequired}` |
| `tool_policy.go` | `ToolPolicy.Resolve()` → `ResolvedToolPolicy{AllowedTools, MaxConcurrency}` |
| `verify_recovery.go` | `VerifyEngine`, `RecoveryEngine`, `CheckpointStore` (in-memory, thread-safe) |
| `stuck_detector.go` | `StuckDetector.Detect(ObservationWindow)` → `[]StuckSignal` with `SuggestMutation` |
| `kernel.go` | `ExecutionKernel`: `ResolveIteration()`, `ObserveIteration()`, mutation tracking |
| `helpers.go` | `SystemExtraFromProfile`, `MaxTurnsFromProfile`, `UnmarshalProfile` |

#### Harness Modes

- **Manual** — user selects tactic flags directly via `HarnessPanel`
- **Auto** — `HarnessComposer` scores `TaskAnalysis` inputs (task type, complexity, risk, repo size, autonomy, model capabilities, cost budget, historical eval score) and emits a `GoalExecProfile`

#### Stuck Detection & Mutation

`StuckDetector` observes each iteration (`ToolsCalled`, `FilesEdited`, `ErrorText`, `TestsFailed`). Detected patterns: repeated errors, repeated test failures, file oscillation, no progress, repeated tool sequences. Each detection yields a `StuckSignal` with `SuggestMutation` (e.g. `Direct→PlanExecute`, add `IndependentReview`, add `RepositoryMap`, `ModelFallback`). Mutations are recorded as `HarnessMutationRow{GoalID, Iteration, Reason, OldProfile, NewProfile, Result}` and persisted to `harness_mutations` table.

#### Persistence

- `goals.harness_mode` + `goals.harness_profile` columns (TEXT, NOT NULL DEFAULT '')
- `cards.harness_mode` + `cards.harness_profile` columns  
- `harness_mutations` table with `(id, goal_id, card_id, iteration, reason, old_profile, new_profile, result, created_at)`
- Store functions: `SaveGoalHarness`, `LoadGoalHarness`, `SaveCardHarness`, `LoadCardHarness`, `InsertHarnessMutation`, `ListHarnessMutations`
- ALTER TABLE migration runs outside transaction (SQLite constraint); duplicate-column errors silently ignored

#### RPC Methods

| Method | Description |
|---|---|
| `harness.get` | Load profile by goalId or cardId |
| `harness.set` | Persist manual profile |
| `harness.mutations` | List mutation history for a goal |
| `harness.compose` | Auto-compose profile from TaskAnalysis JSON |

#### Integration Points

- `goal/engine.go` — `Drive()` loads/composes harness, calls `ExecutionKernel.ResolveIteration()` per iteration, feeds `StuckDetector`, persists mutations, loops until judge approves
- `orchestrator/orchestrator.go` — `Dispatch()` resolves card harness (Auto → compose, Manual → load), applies `SystemExtra` + `MaxTurns` from profile
- `agent/runtime.go` — `RunResult` extended with `ToolsCalled []string` + `FilesEdited []string` for stuck detection
- `judge/engine.go` — `RunGates()` public method runs quality gates without judging (used by goal engine pre-judge)
- `goal/engine.go` — `listRepoFiles()` real implementation: `filepath.WalkDir` with skip list (`.git`, `vendor`, `node_modules`, `.next`, `dist`, `build`), capped at 2000 files

```
go build ./...    ✓
go vet ./...      ✓
go test ./...     ✓  (30 packages, 0 failures)
tsc --noEmit      ✓  (0 errors)
vitest run        ✓  (5 tests, 2 suites)
```