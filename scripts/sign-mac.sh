#!/usr/bin/env bash
# Rove Code.app'i imzalar (gerekirse notarize eder).
#   ROVECODE_SIGN_ID         "Developer ID Application: Ad (TEAMID)"; yoksa ad-hoc
#   ROVECODE_NOTARY_PROFILE  notarytool store-credentials ile kaydedilen profil
set -euo pipefail

APP="${1:?kullanım: sign-mac.sh <Rove Code.app>}"
ID="${ROVECODE_SIGN_ID:--}"
ENT="$(cd "$(dirname "$0")" && pwd)/entitlements.plist"

if [ "$ID" = "-" ]; then
  # içeri eklenen daemon bundle mührünü bozar; ad-hoc olarak yeniden mühürle
  codesign --force --deep --sign - "$APP"
  echo "[sign] ad-hoc: $APP"
  exit 0
fi

# içten dışa: önce daemon, sonra ana program ve bundle
codesign --force --timestamp --options runtime --entitlements "$ENT" --sign "$ID" "$APP/Contents/MacOS/rovecode"
codesign --force --timestamp --options runtime --entitlements "$ENT" --sign "$ID" "$APP"
codesign --verify --strict --deep "$APP"
echo "[sign] $ID: $APP"

if [ -n "${ROVECODE_NOTARY_PROFILE:-}" ]; then
  ZIP="$(mktemp -d "${TMPDIR:-/tmp}/rovecode-notary.XXXXXX")/RoveCode.zip"
  ditto -c -k --keepParent "$APP" "$ZIP"
  xcrun notarytool submit "$ZIP" --keychain-profile "$ROVECODE_NOTARY_PROFILE" --wait
  xcrun stapler staple "$APP"
  rm -rf "$(dirname "$ZIP")"
  echo "[sign] notarized"
fi
