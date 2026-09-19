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

# clang/cgo refuse to compile until the SDK license is agreed.
# xcodebuild -checkFirstLaunchStatus can miss this; probe a real compile.
check_xcode_license() {
  local out
  out="$(echo 'int main(void){return 0;}' | cc -x c - -o /tmp/rovecode-cc-check 2>&1 || true)"
  rm -f /tmp/rovecode-cc-check
  if echo "$out" | grep -qi "license"; then
    echo ""
    echo "Xcode / Apple SDK license is not agreed yet."
    echo "One command (no paging — just your password):"
    echo ""
    echo "  sudo xcodebuild -license accept"
    echo ""
    echo "Then re-run this installer."
    fail "Xcode license not agreed."
  fi
}

# Prefer Homebrew git — it does not go through the Xcode license stub.
prefer_brew_git() {
  if [ -x /opt/homebrew/bin/git ]; then
    export PATH="/opt/homebrew/bin:$PATH"
  elif [ -x /usr/local/bin/git ]; then
    export PATH="/usr/local/bin:$PATH"
  fi
}

fetch_tarball() {
  local repo="$1" branch="$2" dest="$3"
  local url="https://codeload.github.com/${repo}/tar.gz/refs/heads/${branch}"
  local tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/rovecode-src.XXXXXX")"
  log "Downloading source tarball…"
  curl -fsSL "$url" -o "$tmp/src.tgz"
  tar -xzf "$tmp/src.tgz" -C "$tmp"
  local unpacked
  unpacked="$(find "$tmp" -mindepth 1 -maxdepth 1 -type d | head -1)"
  [ -n "$unpacked" ] || fail "Source tarball was empty."
  rm -rf "$dest"
  mkdir -p "$(dirname "$dest")"
  mv "$unpacked" "$dest"
  rm -rf "$tmp"
}

REPO="${ROVECODE_REPO:-Kayra-ML/Rove_cli}"
BRANCH="${ROVECODE_BRANCH:-main}"
SRC_DIR="${ROVECODE_SRC:-$HOME/Library/Caches/RoveCode/src}"

prefer_brew_git

log "Fetching Rove Code source ($REPO@$BRANCH)…"
mkdir -p "$(dirname "$SRC_DIR")"

GIT_BIN="$(command -v git || true)"
USE_GIT=0
if [ -n "$GIT_BIN" ]; then
  if echo "$GIT_BIN" | grep -qE '/(opt/homebrew|usr/local)/bin/git$'; then
    USE_GIT=1
  elif "$GIT_BIN" --version >/dev/null 2>&1; then
    USE_GIT=1
  fi
fi

if [ "$USE_GIT" -eq 1 ] && [ -d "$SRC_DIR/.git" ]; then
  git -C "$SRC_DIR" fetch --depth 1 origin "$BRANCH"
  git -C "$SRC_DIR" checkout -f "origin/$BRANCH"
elif [ "$USE_GIT" -eq 1 ]; then
  rm -rf "$SRC_DIR"
  git clone --depth 1 --branch "$BRANCH" "https://github.com/${REPO}.git" "$SRC_DIR"
else
  fetch_tarball "$REPO" "$BRANCH" "$SRC_DIR"
fi
ok "source: $SRC_DIR"

# Wails / clang still need a working Apple toolchain.
check_xcode_license

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
