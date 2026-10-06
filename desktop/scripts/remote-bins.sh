#!/bin/sh
# Builds the rovecode binaries the desktop app installs on servers
# (linux amd64/arm64), gzip-compressed, into desktop/remotebin. Run by Wails
# before each build (wails.json preBuildHooks); safe to run by hand.
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/.." && pwd)"
out="$here/remotebin"
mkdir -p "$out"
cd "$root"
for arch in amd64 arm64; do
  echo "remote-bins: linux/$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "-s -w" -o "$out/rovecode-linux-$arch" ./cmd/sextant
  gzip -9f "$out/rovecode-linux-$arch"
done
