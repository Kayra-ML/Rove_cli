#!/bin/sh
set -eu

REPO="${ROVECODE_REPO:-Kayra-ML/Rove_cli}"
BINARY="rovecode"
INSTALL_DIR="${ROVECODE_INSTALL_DIR:-/usr/local/bin}"

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)  GOOS="linux" ;;
  Darwin) GOOS="darwin" ;;
  *)
    printf 'Unsupported OS: %s\n' "$OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64)        GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *)
    printf 'Unsupported architecture: %s\n' "$ARCH" >&2
    exit 1
    ;;
esac

ASSET="${BINARY}-${GOOS}-${GOARCH}"
if [ -n "${ROVECODE_VERSION:-}" ]; then
  LATEST="$ROVECODE_VERSION"
else
  LATEST="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
    | sed -n '1p')"
fi

if [ -z "$LATEST" ]; then
  printf 'Could not determine the latest Rove Code release.\n' >&2
  exit 1
fi

BASE_URL="${ROVECODE_BASE_URL:-https://github.com/${REPO}/releases/download/${LATEST}}"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/rovecode.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

printf 'Installing Rove Code %s (%s/%s)...\n' "$LATEST" "$GOOS" "$GOARCH"
curl -fsSL "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${ASSET}"
curl -fsSL "${BASE_URL}/SHA256SUMS" -o "${TMP_DIR}/SHA256SUMS"

EXPECTED="$(sed -n "s/^\([0-9a-fA-F][0-9a-fA-F]*\)[[:space:]][[:space:]]*${ASSET}$/\1/p" "${TMP_DIR}/SHA256SUMS")"
if [ -z "$EXPECTED" ]; then
  printf 'Checksum for %s is missing from the release.\n' "$ASSET" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP_DIR}/${ASSET}" | sed 's/[[:space:]].*//')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMP_DIR}/${ASSET}" | sed 's/[[:space:]].*//')"
else
  printf 'A SHA-256 tool is required (sha256sum or shasum).\n' >&2
  exit 1
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  printf 'Checksum verification failed for %s.\n' "$ASSET" >&2
  exit 1
fi

chmod 0755 "${TMP_DIR}/${ASSET}"
if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "${TMP_DIR}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
else
  printf 'Installing to %s (sudo required)...\n' "$INSTALL_DIR"
  sudo mkdir -p "$INSTALL_DIR"
  sudo install -m 0755 "${TMP_DIR}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
fi

INSTALLED_VERSION="$("${INSTALL_DIR}/${BINARY}" --version)"
printf '\nInstalled: %s\nPath: %s/%s\nRun: rovecode\n' "$INSTALLED_VERSION" "$INSTALL_DIR" "$BINARY"
