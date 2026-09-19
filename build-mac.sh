#!/usr/bin/env bash
# Rove Code — macOS desktop build
# Çalıştır: chmod +x build-mac.sh && ./build-mac.sh
set -e

CYAN='\033[0;36m'; GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'
log()  { echo -e "${CYAN}[rovecode]${NC} $1"; }
ok()   { echo -e "${GREEN}[ok]${NC} $1"; }
fail() { echo -e "${RED}[fail]${NC} $1"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

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
    fail "Xcode license not agreed."
  fi
}
check_xcode_license

# ── 1. Homebrew ───────────────────────────────────────────────────────────────
if ! command -v brew &>/dev/null; then
  log "Homebrew kuruluyor..."
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
ok "brew mevcut"

# ── 2. Go ─────────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  log "Go kuruluyor..."
  brew install go
fi
GO_VERSION=$(go version | awk '{print $3}')
log "Go: $GO_VERSION"
# Minimum go 1.21
MAJOR=$(echo $GO_VERSION | sed 's/go//' | cut -d. -f1)
MINOR=$(echo $GO_VERSION | sed 's/go//' | cut -d. -f2)
if [[ $MAJOR -lt 1 ]] || [[ $MAJOR -eq 1 && $MINOR -lt 21 ]]; then
  log "Go 1.21+ gerekli, güncelleniyor..."
  brew upgrade go || brew install go
fi
ok "Go hazır"

# ── 3. Node ───────────────────────────────────────────────────────────────────
if ! command -v node &>/dev/null; then
  log "Node.js kuruluyor..."
  brew install node
fi
ok "Node: $(node --version)"

# ── 4. Wails CLI ──────────────────────────────────────────────────────────────
if ! command -v wails &>/dev/null; then
  log "Wails CLI kuruluyor..."
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  # PATH'e ekle
  export PATH="$PATH:$(go env GOPATH)/bin"
fi
if ! command -v wails &>/dev/null; then
  export PATH="$PATH:$(go env GOPATH)/bin"
fi
ok "Wails: $(wails version 2>/dev/null || echo 'kurulu')"

# ── 5. Frontend bağımlılıkları ────────────────────────────────────────────────
log "Frontend npm install..."
cd "$SCRIPT_DIR/desktop/frontend"
npm install --silent
ok "node_modules hazır"

# ── 6. Wails build ───────────────────────────────────────────────────────────
cd "$SCRIPT_DIR/desktop"
log "Wails build başlıyor (bu 2-3 dakika sürebilir)..."
wails build -platform darwin/universal -clean 2>&1
# darwin/universal = hem arm64 (M1/M2/M3) hem amd64 (Intel) destekler

APP_PATH="$SCRIPT_DIR/desktop/build/bin/Rove Code.app"
if [ ! -d "$APP_PATH" ]; then
  # Wails bazen sadece darwin adıyla koyar
  APP_PATH=$(find "$SCRIPT_DIR/desktop/build/bin" -name "*.app" | head -1)
fi
if [ -z "$APP_PATH" ] || [ ! -d "$APP_PATH" ]; then
  fail "Build başarısız — .app bulunamadı. Çıktıyı yukarıdan kontrol et."
fi
ok "App bundle: $APP_PATH"

# ── 7. DMG oluştur ────────────────────────────────────────────────────────────
log "DMG oluşturuluyor..."
DMG_PATH="$SCRIPT_DIR/RoveCode.dmg"
APP_NAME=$(basename "$APP_PATH")

# Geçici mount klasörü
TMP_DMG="$SCRIPT_DIR/tmp_dmg"
rm -rf "$TMP_DMG"
mkdir -p "$TMP_DMG"
cp -R "$APP_PATH" "$TMP_DMG/"
# Applications symlink — standart DMG drag-install deneyimi
ln -sf /Applications "$TMP_DMG/Applications"

hdiutil create \
  -volname "Rove Code" \
  -srcfolder "$TMP_DMG" \
  -ov \
  -format UDZO \
  "$DMG_PATH" \
  2>&1

rm -rf "$TMP_DMG"

ok "DMG hazır: $DMG_PATH"
echo ""
echo -e "${GREEN}══════════════════════════════════════${NC}"
echo -e "${GREEN}  RoveCode.dmg oluşturuldu!${NC}"
echo -e "${GREEN}  Konum: $DMG_PATH${NC}"
echo -e "${GREEN}  DMG'yi aç → Rove Code.app'i Applications'a sürükle.${NC}"
echo -e "${GREEN}  Sonra: rovecode${NC}"
echo -e "${GREEN}══════════════════════════════════════${NC}"