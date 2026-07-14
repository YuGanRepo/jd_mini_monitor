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
      npm install
      npm run build
    } finally {
      Pop-Location
    }

    if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
      throw "Wails CLI is not installed. Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
    }
    wails build
  }

  if (-not $DesktopOnly) {
    go build -trimpath -ldflags "-s -w" -o (Join-Path $dist "mini-proxy.exe") ./cmd/mini-proxy
  }
} finally {
  Pop-Location
}
