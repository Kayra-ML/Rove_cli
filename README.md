# Rove CLI — Aether

> A local-first AI agent desktop. Single binary. No cloud required.

Aether is a Go + Wails desktop application that bundles an AI agent core, a kanban task manager, a terminal multiplexer, a skill/plugin marketplace, and a session memory system — all accessible from both a native desktop UI and a CLI.

---

## Architecture

```
aether/
├── cmd/aether/          # CLI entrypoint
├── internal/
│   ├── core/            # Agent lifecycle, health, automation engine
│   ├── rpc/             # JSON-RPC server (WebSocket + HTTP)
│   ├── store/           # SQLite persistence (sessions, cards, goals, KV)
│   ├── gitwt/           # Git worktree helpers + diff
│   ├── marketplace/     # Skill catalog + bundled RoveCode plugins
│   ├── skill/           # Skill runtime
│   └── automation/      # Automation job engine (sweep / assign / drive)
├── pkg/protocol/        # Shared RPC method constants
├── desktop/
│   ├── app.go           # Wails app bindings
│   └── frontend/        # React + TypeScript UI
│       └── src/
│           ├── app/     # UI panels (Chat, Kanban, Terminal, Settings…)
│           ├── hooks/   # useApi, usePrefs
│           └── lib/     # rpc, i18n, slash, toast, types
└── build-mac.sh         # macOS DMG builder
```

## Features

- **Chat** — multi-session AI chat with markdown rendering, slash commands, artifact diffing
- **Kanban** — cards linked to sessions; drag, assign, column collapse
- **Terminal** — PTY multiplexer with SSH support; restart / detach
- **Memory** — scoped key-value notes (global / workspace / session / agent)
- **Marketplace** — 11 RoveCode plugins, 72 skills, bundled via Go embed
- **Automation** — background sweep / assign / drive engine with 5-second tick
- **Cmd+K** — command palette: slash commands + quick navigation
- **Usage** — live token / agent / uptime card

## Slash Commands

| Command | Description |
|---------|-------------|
| `/new` | New chat session |
| `/undo` | Truncate last AI reply |
| `/redo` | Re-run last prompt |
| `/review` | Send to review |
| `/stop` | Stop running agent |
| `/clear` | Clear chat |
| `/run` | Trigger automation sweep |
| `/help` | Show all commands |

## Getting Started

### Prerequisites

- Go 1.22+
- Node.js 20+
- Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

### Run (development)

```bash
cd desktop/frontend && npm install
wails dev
```

### Build (macOS DMG)

```bash
./build-mac.sh
```

### Run CLI only

```bash
go run ./cmd/aether serve
```

## License

[MIT](LICENSE)