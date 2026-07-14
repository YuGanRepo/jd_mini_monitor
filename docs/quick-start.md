# Quick Start

## 1. Build the desktop exe

Install Go 1.22+, Node.js, npm, and Wails:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Build the Wails desktop app:

```powershell
.\scripts\build.ps1
```

The Wails output is written by the Wails toolchain under `build\bin`.

To build only the CLI fallback:

```powershell
go build -o dist/mini-proxy.exe ./cmd/mini-proxy
```

## 2. Open Mini Proxy Desktop

Run the generated desktop exe and use the main window to manage proxy state, certificate trust, rules, and button automation.

## 3. Install the local root certificate

Use the Certificate panel in the desktop app. The equivalent CLI commands are:

```powershell
.\dist\mini-proxy.exe cert-status
.\dist\mini-proxy.exe install-cert
```

## 4. Add an interception rule

Use the Rules Editor panel or edit `configs/example.rules.json` and set the target `host`, `method`, `path`, and response `body`.

Example action:

```json
{
  "type": "mock",
  "status": 200,
  "contentType": "application/json; charset=utf-8",
  "body": "{\"ok\":true}"
}
```

## 5. Start the proxy

Use the Proxy Control panel and enable Windows system proxy if the target app honors system proxy settings. The equivalent CLI command is:

```powershell
.\dist\mini-proxy.exe serve -rules configs/example.rules.json -system-proxy
```

The command sets Windows system proxy while it runs and restores the previous proxy settings when it exits normally.

## 6. Test an intercepted API

Use a browser or a tool that honors the Windows system proxy. For curl, pass the proxy explicitly:

```powershell
curl.exe -x http://127.0.0.1:8899 https://api.example.com/v1/user/profile
```

## 7. Inspect window buttons

Edit `configs/example.automation.json` so the `window` selector points at your target app, then use the Button Automation panel's Inspect button. The equivalent CLI command is:

```powershell
.\dist\mini-proxy.exe uiauto-inspect -config configs/example.automation.json
```

Use returned button `name` or `automationId` values in the automation steps.

## 8. Run button automation

Use the Button Automation panel's Run Sequence button. The equivalent CLI command is:

```powershell
.\dist\mini-proxy.exe uiauto-run -config configs/example.automation.json
```

Use inspect mode first. Only enable `fallbackToCursor` when UI Automation `InvokePattern` is unavailable and coordinate clicking is acceptable.
