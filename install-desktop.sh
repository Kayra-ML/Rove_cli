#!/usr/bin/env bash
# Rove Code — macOS desktop (.app) installer
# Run from anywhere:
#   curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install-desktop.sh | bash
set -euo pipefail

CYAN='\033[0;36m'; GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'
log()  { echo -e "${CYAN}[rovecode]${NC} $1"; }
ok()   { echo -e "${GREEN}[ok]${NC} $1"; }
fail() { echo -e "${RED}[fail]${NC} $1"; exit 1; }

if [ "$(uname -s)" != "Darwin" ]; then
  fail "Desktop installer is macOS-only."
fi

REPO="${ROVECODE_REPO:-Kayra-ML/Rove_cli}"
BRANCH="${ROVECODE_BRANCH:-main}"
SRC_DIR="${ROVECODE_SRC:-$HOME/Library/Caches/RoveCode/src}"

if ! command -v git >/dev/null 2>&1; then
  if command -v brew >/dev/null 2>&1; then
    brew install git
  else
    fail "git required. Install Xcode Command Line Tools: xcode-select --install"
  fi
fi

log "Fetching Rove Code source ($REPO@$BRANCH)…"
mkdir -p "$(dirname "$SRC_DIR")"
if [ -d "$SRC_DIR/.git" ]; then
  git -C "$SRC_DIR" fetch --depth 1 origin "$BRANCH"
  git -C "$SRC_DIR" checkout -f "origin/$BRANCH"
else
  rm -rf "$SRC_DIR"
  git clone --depth 1 --branch "$BRANCH" "https://github.com/${REPO}.git" "$SRC_DIR"
fi
ok "source: $SRC_DIR"

chmod +x "$SRC_DIR/build-mac.sh"
"$SRC_DIR/build-mac.sh"

APP_SRC=""
if [ -d "$SRC_DIR/desktop/build/bin/Rove Code.app" ]; then
  APP_SRC="$SRC_DIR/desktop/build/bin/Rove Code.app"
else
  APP_SRC="$(find "$SRC_DIR/desktop/build/bin" -name "*.app" -maxdepth 1 | head -1 || true)"
fi
[ -n "$APP_SRC" ] && [ -d "$APP_SRC" ] || fail "Build finished but no .app was produced."

DEST="/Applications/Rove Code.app"
log "Installing to $DEST…"
rm -rf "$DEST"
if mkdir -p /Applications && [ -w /Applications ]; then
  cp -R "$APP_SRC" "$DEST"
else
  sudo rm -rf "$DEST"
  sudo cp -R "$APP_SRC" "$DEST"
fi
ok "installed: $DEST"

echo ""
echo -e "${GREEN}Rove Code.app is in /Applications.${NC}"
echo "Run:  rovecode"
echo "Or open Rove Code from Spotlight / Dock."
