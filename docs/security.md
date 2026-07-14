# Security Notes

Mini Proxy performs local HTTPS interception only after you explicitly install its root certificate into the current Windows user's trusted root store.

## Certificate Handling

- The root certificate and private key are stored under the current user's config directory, usually `%APPDATA%\MiniProxy\certs`.
- `install-cert` uses `certutil -user -addstore Root`.
- `uninstall-cert` uses `certutil -user -delstore Root` with the generated certificate thumbprint.
- Do not share the generated root private key.

## Proxy Scope

- `serve -system-proxy` changes the current user's Windows proxy settings while the process runs.
- The previous proxy state is saved before enabling the proxy and restored on normal shutdown.
- If the process is killed, run `mini-proxy proxy-restore` to restore the saved settings.

## Rule Safety

- Keep rules specific to the domain and API path you need.
- Avoid broad host suffix rules unless you want all matching subdomains to be MITM-inspected.
- The tool is intended for local testing and workflow automation. Do not use it to capture credentials or other sensitive traffic.

## UI Automation Safety

- Run `uiauto-inspect` before `uiauto-run`.
- Prefer `automationId` or exact `buttonName` selectors over broad text contains matches.
- Keep `repeat` low and add delays for stateful workflows.
- Enable cursor fallback only for windows where coordinate clicking is acceptable.
