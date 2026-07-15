# JD Chrome Plugin - Current Feature Inventory (for Agents)

Last updated: 2026-07-14

This document is a code-based capability inventory for other agents.
Scope is based on current implementation in extension and server code.

## 1. Project Architecture

- Chrome extension (Manifest V3): request capture, SKU parsing, change detection, notification, license gate.
- Popup UI: React + Ant Design + Vite build output under `build/`.
- Backend service (`server/`): License auth + quote/inquiry management + admin UI + MySQL persistence.

## 2. Extension Functional List

### 2.1 Capture and Parsing

- Injects page-context hooks for `fetch` and `XMLHttpRequest` via `content-script.js -> injected.js`.
- Captures only JD cart API traffic matching:
  - Host contains `api.m.jd.com`
  - URL or request body contains `functionId=pcCart_jc_getCurrentCart`
- Sends captured payload to service worker message `JD_CAPTURE_LOG`.
- Stores latest logs in `chrome.storage.local.captureLogs` (max 10).
- Parses cart payload from `resultData.cartInfo.vendors[].sorted[]` where `itemType === 1`.

### 2.2 SKU Cache and View

- Maintains normalized SKU snapshot cache (`skuSnapshots`) for diff detection.
- Maintains popup display cache (`skuViewCache`) for fast rendering.
- Popup "SKU 列表" shows:
  - Basic SKU fields (name, skuId, inventory, prices, links)
  - Discounted price based on configurable discount rate
  - Server quote comparison (single/case price and diff badge)

### 2.3 Change Detection and Notification

- Detects change categories:
  - 价格
  - 库存
  - 优惠
  - 赠品
- Category-level enable/disable via notify category config.
- Stock delta threshold filter for "剩余库存" changes.
- Quote diff filter:
  - Optional, only notifies items where matched diff > threshold
  - Unmatched quote items are not blocked by this filter
- Supports push formats:
  - text
  - markdown

### 2.4 Messaging Channels

- Multi-channel concurrent sending:
  - DingTalk webhook
  - Bark
- Features:
  - Channel test message (`JD_TEST_MESSAGE_CHANNEL`)
  - Manual status push (`JD_PUSH_SYSTEM_STATUS`)
  - Device tag suffix appended to notifications for source tracking

### 2.5 Refresh Automation

- Alarm-based refresh (`jdAutoRefresh`) with configurable interval.
- Error pause/recovery alarm (`jdResumeAfterError`) with configurable pause time.
- Optional `autoEnsureCartTab`:
  - If no cart tab exists, redirect existing JD tab to cart or create new cart tab.
- Tracks runtime state (`refreshRuntimeState`):
  - last trigger
  - next expected
  - last result

### 2.6 Cart Health and Alerts

- Detects and marks cart state:
  - normal
  - busy/invalid/empty variants
- On problematic responses (HTTP errors, missing/invalid body), pauses refresh and sends alert.
- On recovery, sends "购物车恢复正常" message.

### 2.7 License Gate (Critical)

- All business messages are gated by cached license validity check.
- Exempt messages (always allowed):
  - `JD_GET_DEVICE_ID`
  - `JD_ACTIVATE_LICENSE`
  - `JD_VERIFY_LICENSE`
  - `JD_GET_LICENSE_STATE`
  - `JD_DEACTIVATE_LICENSE`
- License details:
  - Device ID is deterministic fingerprint first, random UUID fallback.
  - Server-signed token (ECDSA P-256) is verified client-side via embedded public key.
  - Cached token validation checks:
    - signature
    - device match
    - active status
    - expiry
    - max cache age
    - anti-clock-rollback tolerance
- Popup shows LicenseGate screen until authorized.

### 2.8 Quote Source and Diff Logic

- Current quote source is server only (`/api/quote/match`).
- Extension passes `{ sku, name, key, deviceId }`.
- Result cache persisted in `chrome.storage.local.serverQuoteCache` (10 min TTL).
- Price selection rule:
  - If JD item considered case-level, prefer `casePerUnit`
  - Else prefer `singlePrice`
  - Fallback to available pricef
- Diff computed from discounted JD per-unit price and package divisor.

## 3. Popup UI Functional List

### 3.1 Tabs

- `SKU 列表`
- `系统配置`

### 3.2 System Config Panels

- Message channel config
- Notification settings
- Quote diff filter config
- Refresh config
- Notify category config
- Price calculation config

### 3.3 Header Actions

- Theme switch: native / geek / google
- Push system status
- Open detached popup window
- Clear logs and SKU cache

## 4. Extension Message Contract (JD_*)

Implemented handlers in service worker:

- `JD_CAPTURE_LOG`
- `JD_GET_CAPTURE_LOGS`
- `JD_GET_SKU_VIEW_CACHE`
- `JD_CLEAR_CAPTURE_LOGS`
- `JD_GET_MESSAGE_CHANNEL_CONFIG`
- `JD_SAVE_MESSAGE_CHANNEL_CONFIG`
- `JD_TEST_MESSAGE_CHANNEL`
- `JD_PUSH_SYSTEM_STATUS`
- `JD_GET_REFRESH_CONFIG`
- `JD_GET_REFRESH_RUNTIME`
- `JD_SAVE_REFRESH_CONFIG`
- `JD_GET_NOTIFY_CATEGORY_CONFIG`
- `JD_GET_NOTIFICATION_SETTINGS`
- `JD_SAVE_NOTIFY_CATEGORY_CONFIG`
- `JD_SAVE_NOTIFICATION_SETTINGS`
- `JD_GET_PRICE_CALC_CONFIG`
- `JD_SAVE_PRICE_CALC_CONFIG`
- `JD_GET_CART_ALERT_STATE`
- `JD_OPEN_DETACHED_WINDOW`
- `JD_GET_DEVICE_ID`
- `JD_ACTIVATE_LICENSE`
- `JD_VERIFY_LICENSE`
- `JD_GET_LICENSE_STATE`
- `JD_DEACTIVATE_LICENSE`
- `JD_GET_SERVER_QUOTE`

## 5. Backend Functional List (`server/`)

## 5.1 Public API (license service port)

- `GET /health`
- `GET /api/public-key`
- `POST /api/license/activate`
- `POST /api/license/verify`
- `POST /api/license/auto-unlock`
- `POST /api/quote/match`

`/api/quote/match` flow:

- Verifies license first.
- Records inquiry by `(sku, name)` with dedupe + hit count.
- Returns mapped quote prices or `null` (no auto-match logic).

### 5.2 Admin API (admin service port)

Base auth model:

- `userAuth`: accepts `ADMIN_TOKEN` or `USER_TOKEN`.
- `adminAuth`: accepts `ADMIN_TOKEN` only.

Role endpoint:

- `GET /api/admin/whoami`.

Quote and inquiry management (user/admin allowed):

- `GET/POST /api/admin/quotes`
- `PUT/DELETE /api/admin/quotes/:id`
- `POST /api/admin/quotes/bulk`
- `POST /api/admin/quotes/test-price`
- `POST /api/admin/quotes/cache/clear`
- `GET /api/admin/inquiries`
- `POST /api/admin/inquiries/:id/map`
- `POST /api/admin/inquiries/:id/unmap`
- `DELETE /api/admin/inquiries/:id`

License management (admin only):

- `POST /api/admin/licenses`
- `GET /api/admin/licenses`
- `GET /api/admin/licenses/by-device`
- `PUT /api/admin/licenses/:key`
- `GET /api/admin/licenses/:key/activations`
- `POST /api/admin/licenses/:key/revoke`
- `POST /api/admin/licenses/:key/restore`
- `POST /api/admin/licenses/:key/unbind`
- `POST /api/admin/licenses/:key/extend`

### 5.3 Admin UI (`/admin`)

- Token login and role-sensitive tabs.
- Tabs:
  - 授权码管理 (admin role only)
  - 报价管理
  - 询价映射
- Includes quote cache clear action and quote test-price tool.

## 6. Data Models and Storage

### 6.1 Extension `chrome.storage.local` keys

- `captureLogs`
- `messageChannelConfig`
- `refreshConfig`
- `refreshRuntimeState`
- `skuSnapshots`
- `skuViewCache`
- `notifyCategoryConfig`
- `notificationSettings`
- `priceCalcConfig`
- `cartAlertState`
- `licenseKey`
- `deviceId`
- `licenseState`
- `serverQuoteCache`
- `appTheme`

### 6.2 Server MySQL tables

- `licenses`
- `activations`
- `quotes`
- `inquiries`

Important behavior:

- `quotes` unique key by `name`.
- `inquiries` unique key by `(sku, name)`.
- Quote/inquiry caches are cleared on quote or mapping mutations.

## 7. Runtime Jobs / Alarms

Extension alarms:

- `jdAutoRefresh`
- `jdResumeAfterError`
- `licenseReverify`

Legacy cleanup:

- `syncYzAlarm()` now only clears legacy `yzAutoRefresh` alarm.

## 8. Build and Packaging

Extension:

- Build: `npm run build`
- Output: `build/`
- Static copied files:
  - `manifest.json`
  - `background.js`
  - `content-script.js`
  - `injected.js`
  - `sku-mapping.json`

Packaging scripts:

- `package-extension.ps1` / `package-extension.sh`
- `release-extension.ps1` / `release-extension.sh`
- Optional obfuscation via `tools/obfuscate-extension.mjs`

Server:

- Start: `npm start`
- Key generation: `npm run genkeys`
- Process scripts: `manage.ps1/.sh`
- Docker scripts: `docker.ps1/.sh`

## 9. Current Functional Boundaries (Important for Agents)

- Quote matching is no longer client-side heuristic matching.
- Extension quote result depends on server inquiry mapping.
- License is a hard gate for all business capabilities.
- If license is invalid/cached-invalid, most extension messages return unauthorized.

## 10. Suggested Reading Order for New Agents

1. `AGENT_FEATURES.md` (this file)
2. `README.md`
3. `background.js` (message handlers + gate + jobs)
4. `src/App.jsx` + `src/tabs/*.jsx` (UI behavior)
5. `server/src/index.js` (API entry)
6. `server/src/licenses.js` and `server/src/quotes.js` (business logic)
