param(
  [switch]$CliOnly,
  [switch]$DesktopOnly
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Push-Location $root
try {
  if (-not $DesktopOnly) {
    go test ./...
  }

  if (-not $CliOnly) {
    Push-Location (Join-Path $root "frontend")
    try {
      npm ci
      npm run build
    } finally {
      Pop-Location
    }

    if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
      throw "Wails CLI is not installed. Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
    }
    wails build

	$desktopConfigDir = Join-Path $root "build\bin\configs"
	New-Item -ItemType Directory -Force -Path $desktopConfigDir | Out-Null
	Copy-Item -Path (Join-Path $root "configs\*") -Destination $desktopConfigDir -Recurse -Force
  }

  if (-not $DesktopOnly) {
    go build -trimpath -ldflags "-s -w" -o (Join-Path $dist "mini-proxy.exe") ./cmd/mini-proxy
	$cliConfigDir = Join-Path $dist "configs"
	New-Item -ItemType Directory -Force -Path $cliConfigDir | Out-Null
	Copy-Item -Path (Join-Path $root "configs\*") -Destination $cliConfigDir -Recurse -Force
  }
} finally {
  Pop-Location
}
