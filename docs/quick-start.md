# Quick Start

## 1. Build the desktop exe

Install Go 1.25+, Node.js 22.12+ (or 20.19+), npm, and Wails:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Build the Wails desktop app:

```powershell
.\scripts\build.ps1
```

The Wails output is written under `build\bin`. The build script copies the
default JSON files to `build\bin\configs`; the executable therefore works when
started from a shortcut or a different working directory.

To build only the CLI fallback:

```powershell
go build -o dist/mini-proxy.exe ./cmd/mini-proxy
```

## 2. Open Mini Proxy Desktop

Run the generated desktop exe. On first use, activate a license key issued by
the configured license server. The main window then provides proxy state,
certificate trust, request logs, SKU data, DingTalk notification settings, and
JD automation.

Startup setup is idempotent:

- The current-user root certificate is checked automatically. If the same
  thumbprint is already trusted, installation is skipped.
- With a valid cached license, the local proxy starts automatically. Windows
  proxy registry values are applied only after the listener succeeds.
- If `ProxyEnable`, `ProxyServer`, and `ProxyOverride` already match, registry
  writes and the WinInet settings refresh are skipped.
- Without a valid cached license, startup does not change Windows proxy values.
  Activate the license and use the Start button once; later launches auto-start.

## 3. Install the local root certificate

Use the Certificate panel in the desktop app. The equivalent CLI commands are:

```powershell
.\dist\mini-proxy.exe cert-status
.\dist\mini-proxy.exe install-cert
```

## 4. Configure interception rule file

Rules are file-based only. The packaged default is
`build\bin\configs\jd.rules.json`. The desktop resolves this path relative to
the executable when the current working directory does not contain it.

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
.\dist\mini-proxy.exe serve -rules configs/jd.rules.json -system-proxy
```

The command sets Windows system proxy while it runs and restores the previous proxy settings when it exits normally.

## 6. Test an intercepted API

Use a browser or a tool that honors the Windows system proxy. For curl, pass the proxy explicitly:

```powershell
curl.exe -x http://127.0.0.1:8899 https://api.example.com/v1/user/profile
```

## 7. Run JD mini-program automation

Open the JD mini-program window before starting automation. Configure its
window title, host process, cycle count, and delay in the desktop panel. A cycle
uses window-relative coordinates to switch between the cart, "全部", and
"服务" tabs. Set the cycle count to unlimited to run until manually stopped.

Stopping waits for the background task to finish. Coordinate ratios outside
`(0, 1]`, negative cycle counts, and delays outside 0 to 3600 seconds are
rejected before any click occurs.

## 8. Use traditional UI Automation from CLI

Traditional Microsoft UI Automation is retained as a CLI capability for
ordinary desktop controls. Edit `configs/example.automation.json`, then inspect
the target window:

```powershell
.\dist\mini-proxy.exe uiauto-inspect -config configs/example.automation.json
```

Use returned button `name` or `automationId` values in the automation steps.

Run the configured sequence with:

```powershell
.\dist\mini-proxy.exe uiauto-run -config configs/example.automation.json
```

Use inspect mode first. Only enable `fallbackToCursor` when UI Automation `InvokePattern` is unavailable and coordinate clicking is acceptable.

## 9. Diagnostics and cleanup

Runtime logs are stored under `%APPDATA%\MiniProxy\logs`. Interception details
are under `logs\intercepts`:

- `proxy-requests.jsonl`: matched proxy requests
- `sku-events.jsonl`: SKU parse results and errors
- `cartview-response-*.json`: captured response samples, at most 100 files
- `sku-latest.json`: latest persisted SKU snapshot

JSONL and the main log rotate at 10 MiB with up to three backups. Clearing SKU
data in the desktop also clears its persisted snapshot.
