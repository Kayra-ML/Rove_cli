# Rove Code

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![Wails](https://img.shields.io/badge/Wails-React-informational)

**A local-first coding agent with a shared Go daemon, a desktop app, a terminal UI and a CLI.**

Rove Code runs one background process, the daemon, that owns all state: sessions, goals, terminals, secrets and memory. The desktop app and the terminal client are thin front ends over the same daemon, so a session started in one can be continued in the other.

## Features

- **One daemon, several clients.** The Wails + React desktop app, the terminal UI and the CLI all talk to the same local process over JSON-RPC (HTTP and a Unix socket).
- **Goal engine with a judge.** Goals persist in SQLite across restarts. After each agent iteration a judge runs deterministic quality gates (build, test, lint) and returns `DONE`, `CONTINUE` or `BLOCKED`.
- **Subagents with isolated checkouts.** An agent can delegate tasks to subagents. When several tasks run in a git project, each gets its own worktree and returns its changes as a patch for you to apply or discard.
- **Three working modes.** Orchestra (one conversation that delegates), Teamwork (one request planned into parts that run in parallel and are merged) and Terminal (coding-CLI panes that can see each other's work).
- **Real terminals.** The daemon owns PTY processes, keeps a replay buffer and exposes SSH sessions and saved hosts through the same API.
- **Secrets stay out of the database.** API keys live in the OS keychain (macOS Keychain, Linux Secret Service, Windows DPAPI). Provider configs store only a secret ID.
- **MCP and skills.** Built-in tools are exposed as MCP tools, external MCP servers can be registered, and skills are installed from YAML manifests that declare their permissions.

## How it fits together

```
 Desktop (Wails + React)      Terminal UI / CLI
            \                        /
             \    JSON-RPC (HTTP, Unix socket)
              v                    v
        +------------------------------------+
        |          rovecode daemon           |
        |  agent runtime · goal + judge      |
        |  subagents · terminals · SSH       |
        |  skills · MCP · memory · secrets   |
        |  SQLite (WAL) · event bus (SSE)    |
        +------------------------------------+
```

The front ends contain no domain logic. The design decisions and package layout are documented in [ARCHITECTURE.md](ARCHITECTURE.md).

## Install

> Read an install script before piping it into a shell.

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/Burak-ML/Rove_cli/main/install.sh | sh
```

**macOS desktop app.** Run from any directory except the repository. It clones the source, builds `Rove Code.app` and copies it to `/Applications`.

```bash
curl -fsSL https://raw.githubusercontent.com/Burak-ML/Rove_cli/main/install-desktop.sh | bash
```

**Windows 10/11 (PowerShell).** Installs `rovecode-desktop.exe` and `rovecode.exe` to `%LOCALAPPDATA%\RoveCode`, creates a desktop shortcut and adds the folder to `PATH`.

```powershell
Set-ExecutionPolicy -Scope Process Bypass
iwr https://raw.githubusercontent.com/Burak-ML/Rove_cli/main/install-windows.ps1 | iex
```

## Usage

```bash
rovecode              # open the desktop app
rovecode daemon       # run the shared daemon in the foreground
rovecode tui          # terminal UI
rovecode --version

rovecode --add-host alias=user@host:port   # save an SSH host
rovecode --list-hosts
rovecode --host alias                      # connect to a remote daemon
```

## Development

```bash
go build ./...
go vet ./...
go test ./...

# Desktop frontend
cd desktop/frontend && npm test -- --run && npx tsc --noEmit
```

Local desktop builds: `./build-mac.sh` on macOS, `.\build-windows.ps1` on Windows.

## Project layout

| Path | Contents |
|------|----------|
| `cmd/sextant` | `rovecode` entry point: daemon, TUI and desktop launcher |
| `internal/` | agent runtime, goal and judge engines, subagents, terminals, SSH, skills, MCP, memory, secrets, storage |
| `pkg/client`, `pkg/protocol` | Go client and JSON-RPC protocol types |
| `desktop/` | Wails bridge and the React + TypeScript frontend |
| `docs/adr` | architecture decision records |

## License

MIT. See [LICENSE](LICENSE).
