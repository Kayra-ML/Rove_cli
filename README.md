# Rove Code

A local-first coding agent built around a **Go daemon**, a **desktop interface** and **CLI workflows**. The desktop and terminal clients work with shared sessions through the same local process.

### What this project demonstrates

- **Application architecture:** Go backend/daemon with a Wails + React desktop interface
- **Developer workflows:** terminal access, session management and automation
- **Local-first design:** shared state and features coordinated by the daemon

**Project focus:** building developer tools that connect the command line, desktop UI and automation workflows.

> Review the install scripts before running any `curl | sh` command on your machine.

---

## Install

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/Burak-ML/Rove_cli/main/install.sh | sh
```

**macOS desktop app** (from any directory, not the repo):

```bash
curl -fsSL https://raw.githubusercontent.com/Burak-ML/Rove_cli/main/install-desktop.sh | bash
```

That clones the source, builds `Rove Code.app`, and copies it to `/Applications`. Then `rovecode`.

**Windows** (PowerShell, Win 10/11):

```powershell
Set-ExecutionPolicy -Scope Process Bypass
iwr https://raw.githubusercontent.com/Burak-ML/Rove_cli/main/install-windows.ps1 | iex
```

Installs `rovecode-desktop.exe` (Wails GUI) + `rovecode.exe` (daemon/CLI) to `%LOCALAPPDATA%\RoveCode`, creates a Desktop shortcut and adds to PATH.

Then:

```bash
rovecode              # desktop app
rovecode desktop      # same
rovecode daemon       # shared daemon in the foreground
rovecode --version
```

## Layout

- Desktop: Wails + React, Go core and daemon
- Profiles, sessions, SSH tunnels, automations, marketplace live in the daemon

## Develop

```bash
export PATH="$HOME/.local/go/bin:$PATH"
go test ./...
go vet ./...
cd desktop/frontend && npm test -- --run && npx tsc --noEmit
```

### Build desktop locally

| Platform | Command |
|---|---|
| macOS | `chmod +x build-mac.sh && ./build-mac.sh` |
| Windows | `.\build-windows.ps1` (PowerShell) |

---

## License

MIT. See [LICENSE](LICENSE).
