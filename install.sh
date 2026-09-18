#!/bin/sh
set -e

REPO="Kayra-ML/Rove_cli"
BINARY="sextant"
INSTALL_DIR="/usr/local/bin"

# Detect OS and arch
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)  GOOS="linux" ;;
  Darwin) GOOS="darwin" ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64)          GOARCH="amd64" ;;
  arm64|aarch64)   GOARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

ASSET="${BINARY}-${GOOS}-${GOARCH}"
if [ "$GOOS" = "windows" ]; then
  ASSET="${ASSET}.exe"
fi

# Get latest release tag
echo "Fetching latest release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "Could not determine latest release. Check https://github.com/${REPO}/releases"
  exit 1
fi

echo "Installing rovecode ${LATEST} (${GOOS}/${GOARCH})..."

URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"

# Download
TMP="$(mktemp)"
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/rovecode"
else
  echo "Installing to ${INSTALL_DIR} (may require sudo)..."
  sudo mv "$TMP" "${INSTALL_DIR}/rovecode"
fi

echo ""
echo "✓ rovecode installed to ${INSTALL_DIR}/rovecode"
echo ""
echo "Run it:"
echo "  rovecode"
echo ""