# Mini Proxy

Mini Proxy is a Windows-focused Go MVP for two local automation tasks:

- intercept selected HTTPS domain/API responses with a local system proxy and a user-trusted root certificate
- click multiple buttons in a chosen Windows desktop window using Microsoft UI Automation

The project now includes a Wails desktop console plus the original CLI. The desktop app is the primary experience for starting/stopping the proxy, installing the local certificate, editing rules, and running button automation.

## Desktop App

The Wails window provides:

- proxy start/stop with optional Windows system proxy control
- current-user root certificate status, install, and uninstall
- JSON rule editing with validation and formatting
- UI Automation inspect/run controls for configured window button sequences
- runtime paths and latest error/status display

## Desktop Build

Install Go 1.22+, Node.js, npm, and the Wails CLI:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Then build the desktop exe:

```powershell
.\scripts\build.ps1
```

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
mini-proxy serve -addr 127.0.0.1:8899 -rules configs/example.rules.json -system-proxy
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

## Current Limits

- rules are JSON only in this MVP
- only HTTP/1.1 MITM is enabled
- apps that ignore Windows system proxy need separate per-app proxy configuration
- self-drawn/game UIs may not expose buttons through UI Automation
