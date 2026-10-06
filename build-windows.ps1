# Rove Code — Windows desktop build
# Run from the repo root in PowerShell 5.1+ (Win 10/11):
#   Set-ExecutionPolicy -Scope Process Bypass
#   .\build-windows.ps1
#
# Prerequisites installed automatically when missing:
#   - Go  1.21+   (via winget)
#   - Node 20+    (via winget)
#   - Wails CLI   (via go install)
#
# Output: desktop\build\bin\rovecode-desktop.exe  +  RoveCode-windows-amd64.zip

param (
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"

function Log  { Write-Host "[rovecode] $args" -ForegroundColor Cyan }
function Ok   { Write-Host "[ok] $args"       -ForegroundColor Green }
function Fail { Write-Host "[fail] $args"     -ForegroundColor Red; exit 1 }

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

# ── 1. Go ─────────────────────────────────────────────────────────────────────
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Log "Go bulunamadi, winget ile kuruluyor..."
    winget install --id GoLang.Go --silent --accept-package-agreements --accept-source-agreements
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" +
                [System.Environment]::GetEnvironmentVariable("PATH", "User")
}
$goVer = go version
Log "Go: $goVer"
Ok "Go hazir"

# ── 2. Node ───────────────────────────────────────────────────────────────────
if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    Log "Node.js bulunamadi, winget ile kuruluyor..."
    winget install --id OpenJS.NodeJS.LTS --silent --accept-package-agreements --accept-source-agreements
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" +
                [System.Environment]::GetEnvironmentVariable("PATH", "User")
}
Ok "Node: $(node --version)"

# ── 3. Wails CLI ──────────────────────────────────────────────────────────────
if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
    Log "Wails CLI kuruluyor..."
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    $gopath = go env GOPATH
    $env:PATH = "$gopath\bin;$env:PATH"
}
if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
    $gopath = go env GOPATH
    $env:PATH = "$gopath\bin;$env:PATH"
}
Ok "Wails hazir"

# ── 4. Frontend bagimliliklar ────────────────────────────────────────────────
Log "Frontend npm install..."
Set-Location "$ScriptDir\desktop\frontend"
npm install --silent
Ok "node_modules hazir"

# ── 5. Wails build ───────────────────────────────────────────────────────────
Set-Location "$ScriptDir\desktop"
Log "Wails build basliyor (bu 2-5 dakika surebilir)..."

if ($Version -eq "dev") {
    $wailsJson = Get-Content "$ScriptDir\desktop\wails.json" | ConvertFrom-Json
    $Version = "v$($wailsJson.info.productVersion)"
}

$env:GOOS    = "windows"
$env:GOARCH  = "amd64"
$env:VERSION = $Version

wails build -platform windows/amd64 -clean -ldflags "-s -w -X main.version=$Version"

# ── 6. Daemon binary ──────────────────────────────────────────────────────────
Set-Location $ScriptDir
Log "Daemon derleniyor ($Version, windows/amd64)..."
$env:CGO_ENABLED = "0"
$env:GOOS        = "windows"
$env:GOARCH      = "amd64"
go build -trimpath -ldflags "-s -w -X main.version=$Version" `
    -o "desktop\build\bin\rovecode.exe" .\cmd\sextant
Ok "daemon: desktop\build\bin\rovecode.exe"

# ── 7. Zip olustur ────────────────────────────────────────────────────────────
Log "ZIP olusturuluyor..."
$zipName  = "RoveCode-windows-amd64.zip"
$binDir   = "$ScriptDir\desktop\build\bin"
$zipPath  = "$ScriptDir\$zipName"

if (Test-Path $zipPath) { Remove-Item $zipPath -Force }

$filesToZip = @()
$desktopExe = Get-ChildItem "$binDir\*.exe" | Where-Object { $_.Name -ne "rovecode.exe" } | Select-Object -First 1
if ($desktopExe) { $filesToZip += $desktopExe.FullName }
$filesToZip += "$binDir\rovecode.exe"
if (Test-Path "$ScriptDir\README.md")              { $filesToZip += "$ScriptDir\README.md" }
if (Test-Path "$ScriptDir\install-windows.ps1")    { $filesToZip += "$ScriptDir\install-windows.ps1" }

Compress-Archive -Path $filesToZip -DestinationPath $zipPath -Force
Ok "ZIP hazir: $zipPath"

$hash = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
"$hash  $zipName" | Out-File -Encoding ASCII "$ScriptDir\SHA256SUMS-windows.txt"
Ok "Checksum: $hash"

Write-Host ""
Write-Host "======================================" -ForegroundColor Green
Write-Host "  RoveCode-windows-amd64.zip hazir!"   -ForegroundColor Green
Write-Host "  Konum: $zipPath"                      -ForegroundColor Green
Write-Host "  Icindeki .exe'yi calistir -> Rove Code acilir." -ForegroundColor Green
Write-Host "  CLI icin: rovecode.exe daemon / tui"  -ForegroundColor Green
Write-Host "======================================" -ForegroundColor Green
