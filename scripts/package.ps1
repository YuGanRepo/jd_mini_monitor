<#
.SYNOPSIS
  One-click packaging script for mini-proxy (Wails desktop + CLI).

.DESCRIPTION
  Sets up the toolchain PATH (Go, Node, Wails) that is often missing from a fresh
  PowerShell session, configures a China-reachable Go proxy/sumdb, then builds.

  By DEFAULT it builds only the Wails desktop app:
    - build\bin\mini-proxy-desktop.exe  (Wails desktop app)
  Pass -WithCli (or -CliOnly) to also/only build:
    - dist\mini-proxy.exe               (headless CLI)

.PARAMETER WithCli
  Also build the headless CLI in addition to the desktop app.

.PARAMETER CliOnly
  Only build the headless CLI (skip the frontend + Wails build).

.PARAMETER SkipTests
  Skip `go test ./...` before building.

.PARAMETER SkipNpmInstall
  Skip `npm ci` (use the existing frontend\node_modules as-is).

.EXAMPLE
  .\scripts\package.ps1              # desktop app only (default)
  .\scripts\package.ps1 -WithCli     # desktop app + CLI
  .\scripts\package.ps1 -CliOnly -SkipTests
#>
[CmdletBinding()]
param(
  [switch]$WithCli,
  [switch]$CliOnly,
  [switch]$SkipTests,
  [switch]$SkipNpmInstall
)

# Desktop is the default target. It is skipped only when building CLI-only.
$BuildDesktop = -not $CliOnly
# CLI is opt-in (via -WithCli or -CliOnly).
$BuildCli = $WithCli -or $CliOnly

$ErrorActionPreference = "Stop"

function Add-ToPath {
  param([string]$Dir)
  if ($Dir -and (Test-Path $Dir) -and ($env:Path -notlike "*$Dir*")) {
    $env:Path = "$Dir;$env:Path"
  }
}

function Resolve-NodeDir {
  # Prefer an already-working node, otherwise fall back to the nvm4w symlink or a
  # concrete installed version (v24.x) per the repo's known-good setup.
  if (Get-Command node -ErrorAction SilentlyContinue) { return $null }

  $candidates = @(
    "C:\nvm4w\nodejs"
  )
  $nvmRoot = "C:\Users\Administrator\AppData\Local\nvm"
  if (Test-Path $nvmRoot) {
    Get-ChildItem -Path $nvmRoot -Directory -Filter "v2*" -ErrorAction SilentlyContinue |
      Sort-Object Name -Descending |
      ForEach-Object { $candidates += $_.FullName }
  }

  foreach ($c in $candidates) {
    if (Test-Path (Join-Path $c "node.exe")) { return $c }
  }
  return $null
}

Write-Host "==> Configuring toolchain PATH" -ForegroundColor Cyan

# Go
Add-ToPath "C:\Program Files\Go\bin"
# Wails CLI (go install target)
Add-ToPath (Join-Path $env:USERPROFILE "go\bin")
# Node (only if not already resolvable)
$nodeDir = Resolve-NodeDir
if ($nodeDir) { Add-ToPath $nodeDir }

# Verify required tools are present.
$missing = @()
if (-not (Get-Command go -ErrorAction SilentlyContinue))   { $missing += "go (expected C:\Program Files\Go\bin)" }
if ($BuildDesktop) {
  if (-not (Get-Command node -ErrorAction SilentlyContinue))  { $missing += "node (expected C:\nvm4w\nodejs or nvm v24.x)" }
  if (-not (Get-Command wails -ErrorAction SilentlyContinue)) { $missing += "wails (run: go install github.com/wailsapp/wails/v2/cmd/wails@latest)" }
}
if ($missing.Count -gt 0) {
  throw "Missing required tool(s):`n  - " + ($missing -join "`n  - ")
}

# China-reachable Go proxy + sumdb mirror (Google-hosted ones are blocked here).
if (-not $env:GOPROXY -or $env:GOPROXY -eq "https://proxy.golang.org,direct") {
  go env -w GOPROXY=https://goproxy.cn,direct
}
go env -w GOSUMDB=sum.golang.google.cn | Out-Null

Write-Host ("    go    : " + (go version)) -ForegroundColor DarkGray
if ($BuildDesktop) {
  Write-Host ("    node  : " + (node --version)) -ForegroundColor DarkGray
  Write-Host ("    wails : " + ((wails version) -join ' ')) -ForegroundColor DarkGray
}

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Push-Location $root
try {
  if (-not $SkipTests) {
    Write-Host "==> Running Go tests" -ForegroundColor Cyan
    go test ./...
  }

  if ($BuildDesktop) {
    Write-Host "==> Building frontend" -ForegroundColor Cyan
    Push-Location (Join-Path $root "frontend")
    try {
      if (-not $SkipNpmInstall) { npm ci }
      npm run build
    } finally {
      Pop-Location
    }

    Write-Host "==> Building Wails desktop app" -ForegroundColor Cyan
    wails build
    $desktopExe = Join-Path $root "build\bin\mini-proxy-desktop.exe"
    if (Test-Path $desktopExe) {
	  $desktopConfigDir = Join-Path $root "build\bin\configs"
	  New-Item -ItemType Directory -Force -Path $desktopConfigDir | Out-Null
	  Copy-Item -Path (Join-Path $root "configs\*") -Destination $desktopConfigDir -Recurse -Force
      Write-Host ("    Desktop: " + $desktopExe) -ForegroundColor Green
    }
  }

  if ($BuildCli) {
    Write-Host "==> Building CLI" -ForegroundColor Cyan
    $cliExe = Join-Path $dist "mini-proxy.exe"
    go build -trimpath -ldflags "-s -w" -o $cliExe ./cmd/mini-proxy
  $cliConfigDir = Join-Path $dist "configs"
  New-Item -ItemType Directory -Force -Path $cliConfigDir | Out-Null
  Copy-Item -Path (Join-Path $root "configs\*") -Destination $cliConfigDir -Recurse -Force
    Write-Host ("    CLI:     " + $cliExe) -ForegroundColor Green
  }

  Write-Host "==> Packaging complete" -ForegroundColor Cyan
} finally {
  Pop-Location
}
