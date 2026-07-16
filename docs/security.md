# Security Notes

Mini Proxy performs local HTTPS interception only after you explicitly install its root certificate into the current Windows user's trusted root store.

## Certificate Handling

- The root certificate and private key are stored under the current user's config directory, usually `%APPDATA%\MiniProxy\certs`.
- `install-cert` uses `certutil -user -addstore Root`.
- `uninstall-cert` uses `certutil -user -delstore Root` with the generated certificate thumbprint.
- Do not share the generated root private key.
- Desktop startup checks the certificate thumbprint and only invokes
	installation when it is missing.

## Proxy Scope

- `serve -system-proxy` changes the current user's Windows proxy settings while the process runs.
- The previous proxy state is saved before enabling the proxy and restored on normal shutdown.
- Registry values are never pointed at Mini Proxy before its local listener is
	ready. Values that already match are not rewritten.
- If the process exits unexpectedly, the recovery marker is retained and the
	desktop restores the previous state on its next launch.
- `mini-proxy proxy-restore` can be used for manual recovery. A successful
	restore removes the recovery marker.
- Definitive online license rejection or manual license deactivation stops the
  proxy and restores the previous Windows proxy state.

## Rule Safety

- Keep rules specific to the domain and API path you need.
- Avoid broad host suffix rules unless you want all matching subdomains to be MITM-inspected.
- The tool is intended for local testing and workflow automation. Do not use it to capture credentials or other sensitive traffic.

## Diagnostic Data

- Matched request metadata, JD cart response samples, parse events, and the
	latest SKU snapshot are stored under `%APPDATA%\MiniProxy\logs\intercepts`.
- Cart responses can contain product names, quantities, vendor information,
	prices, and stock state. Protect this directory as user data.
- Response samples are limited to 100 files. JSONL and the main log rotate at
	10 MiB with up to three backups.
- Clearing the SKU list removes or replaces the persisted snapshot so cleared
	data is not restored on the next launch.

## License Transport

- License responses are ECDSA-signed and verified locally before persistence.
- The default license endpoint currently uses plain HTTP. Signatures prevent a
	forged authorization state, but HTTP does not protect the license key, device
	ID, or request metadata from observation and does not prevent traffic from
	being blocked.
- Deploy an HTTPS reverse proxy for the license service and switch the default
	endpoint to HTTPS before using the product on untrusted networks.

## UI Automation Safety

- Run `uiauto-inspect` before `uiauto-run`.
- Prefer `automationId` or exact `buttonName` selectors over broad text contains matches.
- Keep `repeat` low and add delays for stateful workflows.
- Enable cursor fallback only for windows where coordinate clicking is acceptable.
