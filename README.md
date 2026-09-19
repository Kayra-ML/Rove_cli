# Rove Code

Local-first coding agent. The desktop app is the product. One daemon, shared sessions.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install.sh | sh
```

Then:

```bash
rovecode              # desktop app
rovecode desktop      # same
rovecode daemon       # shared daemon in the foreground
rovecode --version
```

macOS desktop app (from any directory, not the repo):

```bash
curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install-desktop.sh | bash
```

That clones the source, builds `Rove Code.app`, and copies it to `/Applications`. Then `rovecode`.

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
