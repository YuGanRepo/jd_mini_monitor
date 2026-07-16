// Package license implements the server-verified license gate, kept byte-for-byte
// compatible with the jd-chrome-plugin project so the SAME license server and
// key pair are reused:
//
//   - Canonical token string == server/src/canonical.js
//     [key, deviceId, status, expiresAt, issuedAt, serverTime, nonce] joined by "\n"
//   - Signature == ECDSA P-256 / SHA-256 in IEEE-P1363 (r||s, 64 bytes), base64
//   - Public key == the server's SPKI/DER key (embedded_key.go)
//   - Activation/verification go through the server's public API
//     (/api/license/activate, /verify, /auto-unlock) — see client.go
//
// The cached-validation logic mirrors background.js isLicenseValidCached: it
// anchors expiry/freshness on the server's authoritative serverTime plus the
// monotonic local elapsed time, so a local clock rollback cannot revive an
// expired token.
package license

import (
	"strings"
	"time"
)

const (
	// CacheMaxAge is how long an offline cached verification is trusted before a
	// fresh online verify is required. Matches extension LICENSE_CACHE_MAX_AGE_MS.
	CacheMaxAge = 12 * time.Hour

	// ClockSkewTolerance tolerates small NTP corrections; a larger backward jump
	// is treated as a clock rollback. Matches LICENSE_CLOCK_SKEW_TOLERANCE_MS.
	ClockSkewTolerance = 5 * time.Minute

	// DefaultServerURL is the license API base (same host the extension uses).
	DefaultServerURL = "http://118.196.100.19:8787"
)

// TokenPayload is the signed license payload issued by the server. JSON field
// names match the server exactly; the canonical string is derived from these.
type TokenPayload struct {
	Key        string `json:"key"`
	DeviceID   string `json:"deviceId"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expiresAt"`  // ISO 8601
	IssuedAt   string `json:"issuedAt"`   // ISO 8601
	ServerTime string `json:"serverTime"` // ISO 8601, authoritative
	Nonce      string `json:"nonce"`
}

// CanonicalToken reproduces server/src/canonical.js exactly.
func CanonicalToken(p TokenPayload) string {
	return strings.Join([]string{p.Key, p.DeviceID, p.Status, p.ExpiresAt, p.IssuedAt, p.ServerTime, p.Nonce}, "\n")
}

// State is the persisted signed license (mirrors the extension's licenseState).
type State struct {
	Key            string `json:"key"`
	DeviceID       string `json:"deviceId"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expiresAt"`
	IssuedAt       string `json:"issuedAt"`
	ServerTime     string `json:"serverTime"`
	Nonce          string `json:"nonce"`
	Signature      string `json:"signature"`      // base64 IEEE-P1363
	LastVerifiedAt string `json:"lastVerifiedAt"` // local time the token was applied
}

// Payload extracts the signed payload half of a persisted State.
func (s State) Payload() TokenPayload {
	return TokenPayload{
		Key:        s.Key,
		DeviceID:   s.DeviceID,
		Status:     s.Status,
		ExpiresAt:  s.ExpiresAt,
		IssuedAt:   s.IssuedAt,
		ServerTime: s.ServerTime,
		Nonce:      s.Nonce,
	}
}

// StateFromServer builds a persistable State from a verified server response.
func StateFromServer(p TokenPayload, signature string, now time.Time) State {
	return State{
		Key:            p.Key,
		DeviceID:       p.DeviceID,
		Status:         p.Status,
		ExpiresAt:      p.ExpiresAt,
		IssuedAt:       p.IssuedAt,
		ServerTime:     p.ServerTime,
		Nonce:          p.Nonce,
		Signature:      signature,
		LastVerifiedAt: now.UTC().Format(time.RFC3339Nano),
	}
}

// IsValidCached mirrors background.js isLicenseValidCached: signature + device +
// status + expiry + freshness, anchored on serverTime plus the monotonic local
// elapsed time (so a clock rollback fails closed).
func IsValidCached(state State, deviceID string, now time.Time) bool {
	if state.Signature == "" || state.DeviceID != deviceID || state.Status != "active" {
		return false
	}

	serverTime, ok1 := parseISO(state.ServerTime)
	anchor, ok2 := parseISO(state.LastVerifiedAt)
	if !ok1 || !ok2 {
		return false
	}

	// Elapsed since the token was received locally; a large negative value means
	// the clock was rolled back.
	elapsed := now.Sub(anchor)
	if elapsed < -ClockSkewTolerance {
		return false
	}
	monotonic := elapsed
	if monotonic < 0 {
		monotonic = 0
	}

	effectiveNow := serverTime.Add(monotonic)

	expires, ok3 := parseISO(state.ExpiresAt)
	if !ok3 || !expires.After(effectiveNow) {
		return false
	}

	// Freshness: force an online re-verify past the cache window.
	if monotonic > CacheMaxAge {
		return false
	}

	return VerifySignature(state.Payload(), state.Signature) == nil
}

// parseISO parses the ISO 8601 timestamps the Node server emits (with or without
// fractional seconds).
func parseISO(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
