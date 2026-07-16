# Mini Proxy

Mini Proxy is a Windows-focused Go MVP for two local automation tasks:

- intercept selected HTTPS domain/API responses with a local system proxy and a user-trusted root certificate
- click multiple buttons in a chosen Windows desktop window using Microsoft UI Automation

The project now includes a Wails desktop console plus the original CLI. The desktop app is the primary experience for starting/stopping the proxy, installing the local certificate, viewing interception logs, and running JD mini-program automation.

## Desktop App

The Wails window provides:

- proxy start/stop with optional Windows system proxy control
- current-user root certificate status, install, and uninstall
- file-based JSON rule loading (default: `configs/jd.rules.json`)
- JD mini-program automation controls for tab switching cycles
- JD cart SKU extraction, price-change history, filtering, and persisted snapshots
- categorized SKU change detection (price, stock, promotion, and gift)
- concurrent DingTalk/Bark text or Markdown notifications with thresholds, optional signing, and templates
- server quote comparison and quote-difference notification filtering
- server-verified device-bound license activation with a 12-hour offline cache
- request logs, runtime paths, and latest error/status display

## Desktop Build

Install Go 1.25+, Node.js 22.12+ (or 20.19+), npm, and the Wails CLI:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Then build the desktop exe:

```powershell
.\scripts\build.ps1
```

The desktop release is written to `build\bin`. The build script also copies
`configs` beside the executable, so the app can be launched from any working
directory.

On launch, the desktop checks the current-user root certificate and installs it
only when its thumbprint is missing. With a valid cached license it also starts
the local proxy automatically; Windows proxy settings are changed only after
the listener is ready, and matching registry values are left untouched.

For development:

```powershell
wails dev
```

## CLI Commands

```powershell
mini-proxy version
mini-proxy cert-status
mini-proxy install-cert
mini-proxy uninstall-cert
mini-proxy serve -addr 127.0.0.1:8899 -rules configs/jd.rules.json -system-proxy
mini-proxy proxy-on -addr 127.0.0.1:8899
mini-proxy proxy-restore
mini-proxy uiauto-inspect -config configs/example.automation.json
mini-proxy uiauto-run -config configs/example.automation.json
```

## Build

To build only the CLI exe:

```powershell
go test ./...
go build -o dist/mini-proxy.exe ./cmd/mini-proxy
```

## MVP Scope

- HTTPS CONNECT proxy on `127.0.0.1:8899` by default
- per-host certificate generation from a local root CA
- explicit current-user certificate install/uninstall through `certutil -user`
- rule matching by host, host suffix/glob, method, path, path prefix/regex, query, and headers
- response actions: `mock`, `static`, `modify`, and `passthrough`
- optional Windows system proxy enable/restore
- UI Automation inspect/run for ordinary desktop buttons
- Wails desktop app for operational control
- JD cartview SKU extraction and persisted price-change tracking
- DingTalk price-change notification
- Bark notification, category switches, stock threshold, batching, and device tags
- server quote matching with 10-minute cache and difference filtering
- cancellable coordinate automation for the JD mini-program window
- signed online license activation with offline cache validation

## Current Limits

- rules are JSON only in this MVP
- only HTTP/1.1 MITM is enabled
- apps that ignore Windows system proxy need separate per-app proxy configuration
- self-drawn/game UIs may not expose buttons through UI Automation
- the default license endpoint currently uses HTTP; see `docs/security.md`

See `docs/feature-completeness.md` for the current release-readiness audit,
remaining risks, and Windows acceptance checklist.
See `docs/sku-notification-alignment.md` for the exact parity contract with the
JD Chrome plugin.
