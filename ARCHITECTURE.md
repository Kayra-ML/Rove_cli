# Rove Code — Architecture & Implementation Notes

## Overview

Rove Code is a local-first coding agent. Its **Go Core** is the single
source of truth for all subsystems. The desktop app (`rovecode`) and
headless daemon share the same core.

```
┌──────────────────────────────────────────────────────────────┐
│                        Desktop (Wails)                       │
│              React + TypeScript presentation layer           │
│  (Office, Chat, Teamwork, Terminal, Settings, Maps, SSE)     │
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
│  Subagents          Teamwork Engine   Skill Runtime          │
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

### 4. Three Ways to Work in Code
The app's spaces are **Agent** (internally `office`), **Code** (internally `chat`) and
**Automation** (the maps). A session in Code runs in one of three modes, picked from the
menu of its "+" when it is opened:
- **Orchestra** — one conversation. When the work splits, the chat's model hands
  parts to plain subagents with `team_delegate` (no characters, no standing cast).
- **Teamwork** — one request, planned into parts; each part is an orchestra of
  catalog characters (a conductor and its members), wired by cables and merged at
  the end (`internal/teamwork`).
- **Terminal** — the same session as coding-CLI panes that see each other's work.

### 5. Subagents (Hermes-style delegation, Orca-style checkouts)
Any chat can hand work to subagents with the `team_delegate` tool
(`internal/team/delegate.go`). The model follows Hermes Agent's `delegate_task`:

- A task is a `goal` plus a `context`; each opens a subagent channel of its own
  (`types.SpaceWorker`). Subagents are plain agents: no character, no persona.
- Each subagent starts with a fresh context holding only its task, runs with a
  budget of its own (`childTurns`), and only a clipped report returns to the lead.
- Delegation is one level deep: channels (subagents, terminals, Teamwork players)
  never get the tool. At most `maxParallel` tasks run at once, `maxAssignments` per call.

When a call runs more than one task in a git project, each task gets its own
checkout, after Orca (`gitwt.Snapshot` / `AddWorktree` / `WorkPatch` / `ApplyPatch`):

1. The user's working tree — uncommitted and untracked files included — is
   recorded as a commit without touching their index, tree or branch.
2. Each subagent works in a worktree started from that snapshot, under the data
   directory (never inside the repo, so it never shows in `git status`).
3. A finished subagent's changes come back to the user's tree as a patch, left
   uncommitted. A patch that no longer fits lands not at all: its checkout is
   kept, the lead is told, and the user can apply or discard it.

`team.Roster` tracks every task (queued → running → completed / failed /
interrupted / max_turns) and publishes `subagent.updated`; the app's
`SubagentDock` shows it over the message box, with stop, steer
(`agent.Runtime.Steer`, read at the run's next turn), apply and discard.

### 6. Terminal Architecture (Orca-style)
The **Go Core owns the process**. `terminal.Engine` spawns PTY processes (Linux/macOS)
or ConPTY stubs (Windows), stores output in a ring buffer, and publishes events on the
bus. The desktop uses `xterm.js` only as a renderer — it calls `terminal.attach` to
get the replay buffer, then streams writes/resizes via RPC.

SSH sessions go through `ssh.Manager → terminal.Engine` with the same API.

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
`types.Event` values, each subscriber in publish order from a mailbox of its own (a
goroutine per event shuffled streamed text). The SSE endpoint in the HTTP server subscribes and streams
`data: <json>\n\n` frames. The React frontend uses `EventSource` for live updates
(streaming agent output, terminal data, goal and subagent changes).

The maps page's Context Map reads a chat's history as its context (requests, the files
and commands each drew on, how full the window is). Its assistant talks in a hidden
child of the chat (`types.SpaceMap`, from `ctxmap.assistant`), gets that context as a
brief, and offers edits (summarize, forget from a request on, rename, show on the map)
as `rove-action` blocks the app applies only when the user confirms.

### 10. MCP (Model Context Protocol)
`mcp.Runtime` exposes Go Core tools (read_file, write_file, shell, etc.) as MCP tool
definitions over JSON-RPC. External MCP servers can be registered and their tools
surfaced to agents through the Tool Runtime.

## Package Layout

```
cmd/
  sextant/       `rovecode` entry point: daemon, TUI, desktop launcher
internal/
  agent/         Agent runtime, RunRequest, RunResult
  config/        Config loading, platform paths
  core/          App struct — wires all subsystems together
  daemon/        HTTP+IPC server lifecycle, PID file
  eventbus/      Pub/sub event bus
  gitwt/         Git + worktree operations
  goal/          Goal engine — persistent autonomous goals
  id/            Compact random ID generation
  judge/         Judge engine — DONE/CONTINUE/BLOCKED decisions
  marketplace/   Local skill catalog; publishable to future registry
  mcp/           MCP runtime and JSON-RPC server
  memory/        Memory system — scoped key/content pairs
  permission/    Rule-based permission engine (allow/deny/ask)
  provider/      Provider router, OpenAI-compat adapter, fake provider
  qualitygate/   Deterministic quality gates (build/test/lint/custom)
  rpc/           JSON-RPC dispatcher, HTTP+IPC server
  secrets/       OS-keychain secrets store
  session/       Session CRUD and message history
  skill/         Skill runtime — manifest loading, install, slash commands
  ssh/           SSH session manager
  store/         SQLite persistence (WAL mode)
  team/          Subagents: team_delegate, roster, a checkout per task
  teamwork/      Teamwork plans: parts as orchestras, cables, merge, verification
  terminal/      PTY/ConPTY process engine, ring buffer, attach/detach
  tool/          Tool runtime — read_file, write_file, shell, git tools
  types/         Shared domain types
  workspace/     Workspace open/list, Git branch tracking
pkg/
  client/        Go HTTP+IPC client for CLI and tests
  protocol/      JSON-RPC protocol constants and types
desktop/
  main.go        Wails entry point
  app.go         Wails bridge (RPC proxy only — no agent logic)
  frontend/      React + TypeScript UI
    src/
      app/       Page components (Office, Chat, Teamwork, Terminal, Settings, Maps)
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

# Terminal UI
rovecode tui

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
| `go build ./cmd/sextant` | ✅ |
| `go vet ./...` | ✅ |
| `go test ./... -count=1` (29 packages, 50+ tests) | ✅ |
| `tsc --noEmit` | ✅ 0 errors |
| `vitest run` | ✅ 5/5 |

### 11. OpenAI-Compatible Provider (real HTTP streaming)

`internal/provider` contains `OpenAICompat` — a full SSE streaming HTTP client against any OpenAI-compatible endpoint (OpenAI, Together, Groq, Anthropic compat layer, etc.). The router selects it by name; the `Fake` provider is always registered for tests. API keys are never stored in plaintext — they are looked up at call time via the `KeyLookup` function which reads from the Secrets Manager.

**Connected accounts.** `internal/connect` is the catalog a user adds providers from
(Settings → Providers → Add): agent systems on this computer signed in with the user's own
account (Claude Code, Codex, Antigravity, Hermes), model APIs that take a key (OpenAI,
Anthropic, Gemini, OpenRouter, …) and local servers (OpenClaw's gateway, Ollama). It finds
what is installed and signed in, and opens the system terminal for an install or a
sign-in (the CLI opens the browser) — only the catalog's fixed commands, picked by ID. An
agent system is a provider of kind `agent-cli` (`agent://<system>?access=edits|full`):
`provider.AgentCLI` runs the system's non-interactive mode in the chat's folder, streams its
text back, and keeps one session per chat (`agent-sessions.json`), so later turns resume
it; truncating or compacting the chat drops that session.

### 12. Extended RPC Coverage

New RPC methods added to server and protocol:
- `agent.delete` — removes an agent from the store
- `git.commit` — stage-all + commit in a workspace path
- `git.log` — returns N most recent commits with hash/author/date/message
- `shutdown` — graceful daemon stop (100ms delay, os.Exit)

### 13. Git CommitAll + Log

`internal/gitwt` extended with:
- `CommitAll(path, message)` — runs `git add -A && git commit -m`
- `Log(path, limit)` — parses `git log --pretty=format:...` into `[]GitCommit`

## Frontend Pages (desktop/frontend/src/app/)

| Page | Description |
|---|---|
| `App.tsx` | Root shell: topbar nav (Office, Chat, Automation), chat modes, status bar |
| `Chat.tsx` | SSE-streaming chat with session history |
| `Terminal.tsx` | xterm.js renderer, tabbed sessions, spawn/select |
| `AgentManager.tsx` | Create/edit/delete agents; provider + model selection; system prompt |
| `SkillMarket.tsx` | Installed skills list; local path install; marketplace search + install; permission badges |
| `Settings.tsx` | Provider upsert, goal create, secret management |
| `Workspace.tsx` | File browser + workspace selector |
| `HarnessPanel.tsx` | Harness profile viewer/editor: mode badge, tactic chips, mutation timeline, 5s live poll, Manual/Auto toggle |

### 14. Execution Policy Matrix — Harness System

`internal/harness/` implements the central execution policy system. Every Goal carries a `GoalExecProfile` that determines how the engine thinks, what context it sees, how it executes, which tools it uses, how it verifies, and how it recovers.

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
- `harness_mutations` table with `(id, goal_id, iteration, reason, old_profile, new_profile, result, created_at)`
- Store functions: `SaveGoalHarness`, `LoadGoalHarness`, `InsertHarnessMutation`, `ListHarnessMutations`
- ALTER TABLE migration runs outside transaction (SQLite constraint); duplicate-column errors silently ignored

#### RPC Methods

| Method | Description |
|---|---|
| `harness.get` | Load profile by goalId |
| `harness.set` | Persist manual profile |
| `harness.mutations` | List mutation history for a goal |
| `harness.compose` | Auto-compose profile from TaskAnalysis JSON |

#### Integration Points

- `goal/engine.go` — `Drive()` loads/composes harness, calls `ExecutionKernel.ResolveIteration()` per iteration, feeds `StuckDetector`, persists mutations, loops until judge approves
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
### 15. Packaging, updates and the daemon's environment

- **The app carries its daemon.** `build-mac.sh` (and the release's macOS job) builds `rovecode` into `Rove Code.app/Contents/MacOS/`; `daemon.Command()` prefers the program next to the app. `install-desktop.sh` links `/usr/local/bin/rovecode` to it, so updating the app updates the CLI with no sudo.
- **A stale daemon makes way.** On start the desktop app (`desktop/stale.go`) compares the running daemon's API level and program (`exe`, `exeModTime` in `ping`) with the one it would start, and stops an older one unless agents are running.
- **Updates.** `desktop/update.go` reads the latest GitHub release. A release with `RoveCode-macos-universal.zip` is installed in place after its SHA256SUMS line and code signature check out (a Developer ID app only accepts the same team), then the app restarts. Without that asset the source installer runs in Terminal.
- **Signing.** `scripts/sign-mac.sh` signs ad hoc, or with `ROVECODE_SIGN_ID` (hardened runtime) and notarizes with `ROVECODE_NOTARY_PROFILE`; CI does the same when the `MACOS_*` secrets are set.
- **Logs.** A daemon without a terminal writes to `<data>/logs/rovecode-daemon.log` (rotated at 5 MB, crash stacks included): start, failed RPCs and failed runs.
- **PATH.** An app opened from the Finder gets `/usr/bin:/bin:/usr/sbin:/sbin`; the daemon asks the login shell for the user's PATH at start (`daemon.FixPath`), so agents find Homebrew, node, go and the user's python.
- **The agent knows its machine.** The system prompt has an Environment block (OS, `/bin/sh` syntax, installed tools, which python libraries are missing). A tool call that fails exactly as it already did in the turn is marked so the model changes something instead of repeating it.
- **Secrets key in the keychain.** On macOS the master key lives in the login keychain (account `master@<data dir>`); a key still in `secrets/master.key` is moved there and the file removed once it reads back. A locked keychain never causes a new key while secrets exist.

### 16. Teamwork: several orchestras

- A job is split into parts (`internal/teamwork`). Each part is an **orchestra**: a conductor (the part's first agent; its channel is a `teamwork` child of the chat) and up to three members (`orchestra` children of the conductor's channel). Budgets: 5 parts, 4 players per part, 14 players in all, trimmed one member at a time from the largest orchestra.
- **Cables** are dependencies. A part starts as soon as every part it hangs on is done (not wave by wave), and its brief carries their reports. Where two or more cables meet there is a **junction**; the merge orchestra closes the plan.
- An orchestra with members is played by the engine in three movements, so it works with any model, agent CLIs included: the conductor answers with the split (`{"pieces":[…]}`), the members play their pieces side by side in their own chats, and the conductor finishes with their reports. Every player gets the chat's model.
- A stopped plan continues with `teamwork.retry`: done parts keep their work; the rest play again in the same chats.
- The view (`TeamworkView.tsx`, `lib/teamwork.ts graphOf`) puts the orchestras on the left — cards in columns by wave, cables that show whether work has gone through, junction knots — and the conversation in a panel on the right: the request, the plan and its box. Opening a card or a junction swaps the panel to those orchestras' chats (the player picked from a corner pill) and draws one lit cable from what is open to the panel.
- Agent CLIs now receive Rove's system message (first message of a CLI session), and one-off questions (`ChatRequest.Ask`) run them read-only.

### 17. Agent: the user's own agents, working as a team

- The Agent space (internally `office`) is the user's agents (`AgentProfile`), made in Settings → Office (`OfficePanel.tsx`). The Office side list shows only them; its "+" opens the editor in Settings. A chat opened with an agent is created with `profileIds: [id]` and grouped under it by its persona badge (`profileId`).
- An agent has a logo (`mark` = "shape:face", `color`; drawn by `AgentMark.tsx` from `lib/agentmark.ts`: 8 shapes, 5 faces, 10 colors, a default derived from its id), an optional starting character, a system prompt (`promptMode`: the character's, the character's plus additions, or `own` to replace it — the character's permissions still apply), a model from `model.list` (empty: the chat's), and permissions (`features`: nil = the defaults). `profile.upsert` checks the name, model, character and prompt mode.
- `lib/office.ts` moves the old office (catalog characters picked in this browser) into agents once, and re-points those chats to them.
- The agents work like a team (`internal/staff`, `StaffBoard.tsx`, the space's home):
  - **Title** (`AgentProfile.Title`): what each does; colleagues and the user see it.
  - **Tasks** (`staff.assign`): handed over without opening a chat. A task runs in the background in an Agent-space conversation of its own (persona: the agent), and its clipped report is kept (`staff_tasks`, JSON bodies). The board shows each agent working, with a report waiting, or free (`staff.members`); a finished task notifies (`staff.updated`), reading it marks it seen, a running one can be stopped, and tasks a stopped daemon left running are closed on start.
  - **Notes** (`MemProfile` memory, the `agent_note` tool): a line the agent saves is in every later conversation it has; the user can read, add and delete them on the board.
  - **Colleagues** (the `ask_colleague` tool): an agent hands a part of its job to another by name or title and waits for the report. One level deep: a colleague's request is done by that colleague, never passed on.

### 18. Agents that hold up

- **Reports keep their end.** Agents narrate first and write the result last; reports passed between parts, members and subagents are cut from the front (`clipTail`), never the end.
- **Checked before done.** When a Teamwork plan's parts are done, the merge orchestra (or the only part's conductor) is asked to prove the work by running it — build, tests, a served web page fetched with curl, scripts checked with node — and to end with `{"verified", "checks", "problems"}`. A failed check gets one round of fixes and is checked again; the plan keeps the verdict (`Plan.Verify`) and the view shows it. The merge channel has write permission (the reviewer character alone cannot fix).
- **Permissions reach agent CLIs.** The tools Rove offers (already filtered by the agent's permissions) gate the CLI: Claude Code gets `--disallowedTools` and no bypass when restricted, Codex a read-only sandbox when the agent may not write, Antigravity/Hermes no no-ask mode. Claude Code takes Rove's system message as `--append-system-prompt` on every call.

### 19. Usage: Rove's own count, the provider's beside it

- Every model call (a chat turn, a one-off question, a compaction) goes through `agent.metered` (`internal/agent/meter.go`): it counts what the request really sends — system prompt, history, tool definitions, pictures — and what comes back — text and tool calls — with `provider.MeasureRequest` / `AddReply` (`internal/provider/count.go`). Characters are exact; tokens are estimated from them (about 4 ASCII characters a token, 2 for accented Latin, 1 for CJK, plus framing per message and tool).
- Each call is a row of `usage_ledger`: Rove's count (`sent_*`, `recv_*`) beside what the provider reported (`prompt_tokens`, `completion_tokens`, `cached_tokens`, `reported`), with kind, session, provider, model and duration.
- `usage.report` (`internal/usage`) sums the ledger into local days (every day present) and models; the comparison with the providers is made only over the calls they reported on. Settings → Usage (`UsagePanel.tsx`) leads with Rove's count and shows the gap and its usual causes (hidden prompts and steps of agent CLIs, thinking tokens, cache reads).

### 20. Agents on the Session Map: watches

- An Agent-space agent can stand on the Session Map as a card of its own (node id `agent:<profile>`, placed with `ctxmap.place`). A cable drawn between a chat and an agent — either way — is a **watch** (`internal/staff/watch.go`, `staff.watchSave` / `staff.watches` / `staff.watchDelete`), not a chat-to-chat link.
- When a turn in the watched chat changes files (the run hook, beside the map's own relays), the agent gets a task: the watch's instruction, the changed files that pass its filter, and what the chat's agent said. The task runs like any other (`staff.Assign`), carries `watchId` and `origin`, and reports on the Team board.
- Guards: a file filter, a daily cap (10 by default), one running task per watch, an on/off switch, and no chains — a task's own chat never sets a watch off. Taking either card off the map removes its watches; removing the agent does too.
- Cables are straight lines from edge to edge; one starts from anywhere on a card's edge, or from anywhere on it with ⌥.

### 21. Automations set up by talking

- A chat the user talks in (Code or Agent; never a subagent, a terminal pane or an agent's own task — `staff.CanAutomate`) gets five tools (`internal/staff/automate.go`): `automation_list`, `automation_watch` (an agent watches a chat), `automation_link` (a cable between two chats), `automation_schedule` and `automation_remove`. Agents and chats are found by name or title; what is set up is put on the Session Map and listed on the agent's card on the Team board, where it can be switched off or removed.
- **Schedules** (`internal/staff/schedule.go`, `staff.schedules` / `staff.scheduleSave` / `staff.scheduleDelete`) give an agent a task every N minutes (at least 15) or every day at a local time. The engine ticks every 30 s; a run does not start while the last is still going, a daily one runs once per day, and a new one waits for its first slot.

### 22. Agents that work apart, learn, connect and keep watch

- **Their own checkout** (`internal/staff/isolate.go`): in a git project a task works in a worktree started from a snapshot of the user's tree (uncommitted work included) and its changes wait there (`isolation.state = review`): `staff.diff`, `staff.apply` (lands uncommitted), `staff.discard`, `staff.pr`. A PR is opened from a branch started at HEAD carrying only the task's patch — never the user's uncommitted work — pushed to `origin` and opened with the user's `gh`. A colleague's request (`AssignWait`) lands at once, since the colleague needs it. Outside git a task works in the folder as before.
- **Learning** (`learn.go`): after each task one tool-less call asks for at most two lessons worth keeping; they become notes keyed `learned:`. Past 24 learned notes they are folded to 12. Notes the user wrote are never touched; the board marks learned ones.
- **Integrations**: `AgentProfile.Integrations` lists the MCP servers an agent may use (nil: all). `persona.Resolved.AllowIntegration` filters `mcp_<server>_*` tools beside the feature permissions.
- **Monitors** (`monitor.go`): a shell command checked every N minutes (≥5) in the project's folder; output lines not seen before (hashes remembered) give the agent a task, nothing new costs no tokens, the first check only learns. Presets: GitHub CI failures and new bugs (`gh`), Sentry (`sentry-cli`). Also set up from a chat with `automation_monitor`, which asks like the shell does.
- **Handoffs** (`handoff.go`, `staff.handoffs` / `staff.handoffSave` / `staff.handoffDelete`, the `automation_handoff` tool, an agent-to-agent cable on the Session Map): when an agent finishes a task (done, not a colleague's request), each enabled handoff from it gives the next agent a task with the instruction, the report and the changed files. The next task's checkout starts from a snapshot of the previous one's and keeps its base, so its review holds both pieces of work; the previous task is marked `carried`. Chains stop at three agents and never return to one already in the chain; daily caps and one running task per handoff as with watches.
- Map cables join cards **corner to corner** (the closest pair of corners); the side ports show only on hover.
