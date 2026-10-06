# Rove Code — Windows installer
# Run in PowerShell (Admin recommended for PATH changes):
#   Set-ExecutionPolicy -Scope Process Bypass
#   iwr https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install-windows.ps1 | iex
#
# Installs rovecode-desktop.exe + rovecode.exe (daemon/CLI) to %LOCALAPPDATA%\RoveCode
# and optionally adds them to the user PATH.

$ErrorActionPreference = "Stop"

$REPO        = if ($env:ROVECODE_REPO)    { $env:ROVECODE_REPO }    else { "Kayra-ML/Rove_cli" }
$INSTALL_DIR = if ($env:ROVECODE_INSTALL) { $env:ROVECODE_INSTALL } else { "$env:LOCALAPPDATA\RoveCode" }
$ASSET       = "RoveCode-windows-amd64.zip"

function Log  { Write-Host "[rovecode] $args" -ForegroundColor Cyan }
function Ok   { Write-Host "[ok] $args"       -ForegroundColor Green }
function Fail { Write-Host "[fail] $args"     -ForegroundColor Red; exit 1 }

# ── Latest release ────────────────────────────────────────────────────────────
Log "GitHub'dan son surum bilgisi aliniyor..."
$apiUrl  = "https://api.github.com/repos/$REPO/releases/latest"
$headers = @{ Accept = "application/vnd.github+json" }
try {
    $release = Invoke-RestMethod -Uri $apiUrl -Headers $headers -TimeoutSec 15
} catch {
    Fail "Surum bilgisi alinamadi: $_"
}

$tag      = $release.tag_name
$zipAsset = $release.assets | Where-Object { $_.name -eq $ASSET } | Select-Object -First 1
$sumsAsset = $release.assets | Where-Object { $_.name -eq "SHA256SUMS" } | Select-Object -First 1

if (-not $zipAsset) { Fail "Bu release'de $ASSET bulunamadi." }
Log "Surum: $tag"

# ── Download ──────────────────────────────────────────────────────────────────
$tmp = New-TemporaryFile | ForEach-Object { Remove-Item $_; New-Item -ItemType Directory -Path "$($_.FullName)-dir" }
try {
    $zipPath  = "$tmp\$ASSET"
    $sumsPath = "$tmp\SHA256SUMS"

    Log "Indiriliyor $ASSET..."
    Invoke-WebRequest -Uri $zipAsset.browser_download_url -OutFile $zipPath -UseBasicParsing

    if ($sumsAsset) {
        Log "SHA256SUMS indiriliyor..."
        Invoke-WebRequest -Uri $sumsAsset.browser_download_url -OutFile $sumsPath -UseBasicParsing

        # Checksum dogrulama
        $expected = (Get-Content $sumsPath | Where-Object { $_ -match $ASSET }) -replace '\s.*', ''
        if ($expected) {
            $actual = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
            if ($expected.ToLower() -ne $actual) {
                Fail "Checksum eslesmiyor. Indirme bozuk olabilir."
            }
            Ok "Checksum dogrulandi"
        }
    }

    # ── Extract ───────────────────────────────────────────────────────────────
    Log "Cikartiliyor..."
    $extractDir = "$tmp\extracted"
    Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

    # ── Install ───────────────────────────────────────────────────────────────
    New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null

    Get-ChildItem -Path $extractDir -Filter "*.exe" | ForEach-Object {
        Copy-Item $_.FullName "$INSTALL_DIR\$($_.Name)" -Force
        Log "Kuruldu: $INSTALL_DIR\$($_.Name)"
    }
    Ok "Dosyalar: $INSTALL_DIR"

    # ── Desktop shortcut ──────────────────────────────────────────────────────
    $desktopExe = Get-ChildItem "$INSTALL_DIR\*.exe" | Where-Object { $_.Name -ne "rovecode.exe" } | Select-Object -First 1
    if ($desktopExe) {
        $shortcutPath = "$env:USERPROFILE\Desktop\Rove Code.lnk"
        $wsh = New-Object -ComObject WScript.Shell
        $sc  = $wsh.CreateShortcut($shortcutPath)
        $sc.TargetPath  = $desktopExe.FullName
        $sc.WorkingDirectory = $INSTALL_DIR
        $sc.Description = "Rove Code - Local-first coding agent"
        $sc.Save()
        Ok "Masa ustu kisayolu olusturuldu: $shortcutPath"
    }

    # ── PATH ──────────────────────────────────────────────────────────────────
    $curPath = [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if ($curPath -notlike "*$INSTALL_DIR*") {
        [System.Environment]::SetEnvironmentVariable(
            "PATH", "$curPath;$INSTALL_DIR", "User")
        $env:PATH += ";$INSTALL_DIR"
        Ok "PATH'e eklendi (yeni terminallerde gecerli)"
    }

} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "======================================" -ForegroundColor Green
Write-Host "  Rove Code $tag kuruldu!"              -ForegroundColor Green
Write-Host "  Konum : $INSTALL_DIR"                 -ForegroundColor Green
Write-Host "  Calistir: rovecode-desktop.exe (GUI)" -ForegroundColor Green
Write-Host "         ya da: rovecode.exe tui (CLI)" -ForegroundColor Green
Write-Host "======================================" -ForegroundColor Green
