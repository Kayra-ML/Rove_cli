# Rove Code

Local-first coding agent. One command installs the terminal cockpit. Desktop and daemon share the same sessions.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install.sh | sh
```

Then:

```bash
rovecode              # terminal cockpit
rovecode desktop      # desktop app, after Rove Code.app is installed
rovecode daemon       # shared daemon in the foreground
rovecode --version
```

macOS desktop bundle (separate from the CLI installer):

```bash
./build-mac.sh
```

Drag `Rove Code.app` into `/Applications`, then `rovecode desktop`.

## Layout

- Terminal: chat on the left, plan + usage on the right
- Desktop: Wails + React, same Go core and daemon
- Profiles, sessions, SSH tunnels, automations, marketplace all live in the daemon

## Develop

```bash
export PATH="$HOME/.local/go/bin:$PATH"
go test ./...
go vet ./...
cd desktop/frontend && npm test -- --run && npx tsc --noEmit
```
