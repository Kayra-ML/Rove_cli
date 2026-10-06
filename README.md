# Rove Code

Local-first coding agent. The desktop app is the product. One daemon, shared sessions.

## Install

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install.sh | sh
```

**macOS desktop app** (from any directory, not the repo):

```bash
curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install-desktop.sh | bash
```

That clones the source, builds `Rove Code.app`, and copies it to `/Applications`. Then `rovecode`.

**Windows** (PowerShell, Win 10/11):

```powershell
Set-ExecutionPolicy -Scope Process Bypass
iwr https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install-windows.ps1 | iex
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
