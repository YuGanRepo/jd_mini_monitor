package license

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// signP1363 signs the canonical form of a payload with an IEEE-P1363 (r||s)
// signature, exactly like the Node server (dsaEncoding: 'ieee-p1363').
func signP1363(t *testing.T, key *ecdsa.PrivateKey, payload TokenPayload) string {
	t.Helper()
	digest := sha256.Sum256([]byte(CanonicalToken(payload)))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return base64.StdEncoding.EncodeToString(sig)
}

func embedKey(t *testing.T, key *ecdsa.PrivateKey) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	EmbeddedPublicKey = der
	t.Cleanup(func() { EmbeddedPublicKey = nil })
}

func samplePayload(deviceID string, expiresAt, serverTime time.Time) TokenPayload {
	return TokenPayload{
		Key:        "AAAA-BBBB-CCCC-DDDD",
		DeviceID:   deviceID,
		Status:     "active",
		ExpiresAt:  expiresAt.UTC().Format(time.RFC3339Nano),
		IssuedAt:   serverTime.UTC().Format(time.RFC3339Nano),
		ServerTime: serverTime.UTC().Format(time.RFC3339Nano),
		Nonce:      "0123456789abcdef",
	}
}

func TestCanonicalTokenFormat(t *testing.T) {
	p := TokenPayload{Key: "K", DeviceID: "D", Status: "active", ExpiresAt: "E", IssuedAt: "I", ServerTime: "S", Nonce: "N"}
	if got := CanonicalToken(p); got != "K\nD\nactive\nE\nI\nS\nN" {
		t.Fatalf("canonical mismatch: %q", got)
	}
}

func TestVerifySignatureP1363(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)

	now := time.Now()
	payload := samplePayload("dev-1", now.Add(24*time.Hour), now)
	sig := signP1363(t, key, payload)

	if err := VerifySignature(payload, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// Tamper the payload → must fail.
	payload.Status = "revoked"
	if err := VerifySignature(payload, sig); err == nil {
		t.Fatal("tampered payload should fail verification")
	}
}

func TestVerifySignatureWrongKey(t *testing.T) {
	signer, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, other) // embed a DIFFERENT key

	now := time.Now()
	payload := samplePayload("dev-1", now.Add(24*time.Hour), now)
	sig := signP1363(t, signer, payload)

	if err := VerifySignature(payload, sig); err == nil {
		t.Fatal("signature from a different key must be rejected")
	}
}

func TestVerifySignatureDevMode(t *testing.T) {
	EmbeddedPublicKey = nil // dev mode
	payload := samplePayload("dev-1", time.Now().Add(time.Hour), time.Now())
	if err := VerifySignature(payload, base64.StdEncoding.EncodeToString(make([]byte, 64))); err != nil {
		t.Fatalf("dev mode should accept any signature: %v", err)
	}
}

func TestIsValidCachedFresh(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)

	now := time.Now()
	payload := samplePayload("dev-1", now.Add(30*24*time.Hour), now)
	sig := signP1363(t, key, payload)
	state := StateFromServer(payload, sig, now)

	if !IsValidCached(state, "dev-1", now.Add(time.Hour)) {
		t.Fatal("fresh cached state should be valid")
	}
	// Wrong device.
	if IsValidCached(state, "dev-2", now.Add(time.Hour)) {
		t.Fatal("wrong device must be rejected")
	}
}

func TestIsValidCachedStaleAndRollback(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)

	now := time.Now()
	payload := samplePayload("dev-1", now.Add(30*24*time.Hour), now)
	sig := signP1363(t, key, payload)
	state := StateFromServer(payload, sig, now)

	// Past the cache window → stale.
	if IsValidCached(state, "dev-1", now.Add(CacheMaxAge+time.Minute)) {
		t.Fatal("stale cache (beyond max age) should be rejected")
	}
	// Clock rolled back well before the anchor → rejected.
	if IsValidCached(state, "dev-1", now.Add(-10*time.Minute)) {
		t.Fatal("clock rollback should be rejected")
	}
}

func TestIsValidCachedExpired(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)

	now := time.Now()
	// Token already expired per serverTime anchor.
	payload := samplePayload("dev-1", now.Add(-time.Hour), now)
	sig := signP1363(t, key, payload)
	state := StateFromServer(payload, sig, now)

	if IsValidCached(state, "dev-1", now) {
		t.Fatal("expired token should be rejected")
	}
}

func TestClientActivateVerify(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		now := time.Now()
		payload := TokenPayload{
			Key:        body["key"],
			DeviceID:   body["deviceId"],
			Status:     "active",
			ExpiresAt:  now.Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339Nano),
			IssuedAt:   now.UTC().Format(time.RFC3339Nano),
			ServerTime: now.UTC().Format(time.RFC3339Nano),
			Nonce:      "abc123",
		}
		if payload.Key == "" && r.URL.Path == "/api/license/auto-unlock" {
			payload.Key = "AUTO-KEY0-0000-0000"
		}
		sig := signP1363(t, key, payload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "key": payload.Key, "payload": payload, "signature": sig})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	now := time.Now()

	state, err := client.Activate("AAAA-BBBB-CCCC-DDDD", "dev-1", now)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !IsValidCached(state, "dev-1", now.Add(time.Minute)) {
		t.Fatal("activated state should be valid")
	}

	if _, err := client.Verify("AAAA-BBBB-CCCC-DDDD", "dev-1", now); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, err := client.AutoUnlock("dev-1", now); err != nil {
		t.Fatalf("AutoUnlock: %v", err)
	}
}

func TestClientRejectsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "revoked"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.Activate("K", "dev-1", time.Now()); err == nil || ErrorCode(err) != "revoked" {
		t.Fatalf("expected revoked error, got %v", err)
	}
}

func TestApplyResponseRejectsInactiveAndExpiredPayloads(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)
	now := time.Now().UTC()

	for _, test := range []struct {
		name   string
		code   string
		mutate func(*TokenPayload)
	}{
		{name: "inactive", code: "revoked", mutate: func(payload *TokenPayload) { payload.Status = "revoked" }},
		{name: "expired", code: "expired", mutate: func(payload *TokenPayload) { payload.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339Nano) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := samplePayload("dev-1", now.Add(time.Hour), now)
			test.mutate(&payload)
			response := serverResponse{Payload: &payload, Signature: signP1363(t, key, payload)}
			_, err := applyResponse(response, payload.Key, payload.DeviceID, now)
			var serverErr *ServerError
			if !errors.As(err, &serverErr) || serverErr.Code != test.code {
				t.Fatalf("applyResponse() error = %v, want code %q", err, test.code)
			}
		})
	}
}

func TestClientRejectsDeviceMismatch(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	embedKey(t, key)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		payload := samplePayload("SOMEONE-ELSE", now.Add(time.Hour), now) // wrong device
		sig := signP1363(t, key, payload)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "payload": payload, "signature": sig})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.Activate("AAAA-BBBB-CCCC-DDDD", "dev-1", time.Now()); err == nil || ErrorCode(err) != "device-mismatch" {
		t.Fatalf("device mismatch in signed payload error = %v", err)
	}
}

func TestDeviceIDIsStable(t *testing.T) {
	first := deviceIDFromParts("host", "serial", nil)
	second := deviceIDFromParts("host", "serial", nil)
	if first == "" || first != second {
		t.Fatal("DeviceID not stable")
	}
	if got := deviceIDFromParts("", "", nil); got != "" {
		t.Fatalf("empty hardware fingerprint = %q, want empty fallback signal", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir + "/license-state.json")

	if s, err := store.Load(); err != nil || s.Key != "" {
		t.Fatalf("empty load: %+v err=%v", s, err)
	}

	state := State{Key: "AAAA-BBBB-CCCC-DDDD", DeviceID: "dev-1", Status: "active", Signature: "sig", LastVerifiedAt: "t"}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil || got.Key != "AAAA-BBBB-CCCC-DDDD" {
		t.Fatalf("reload mismatch: %+v err=%v", got, err)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if s, _ := store.Load(); s.Key != "" {
		t.Fatal("state should be cleared")
	}
}

func TestStoreConcurrentSaves(t *testing.T) {
	store := NewStore(t.TempDir() + "/license-state.json")
	const writers = 32
	var waitGroup sync.WaitGroup
	errors := make(chan error, writers)

	for index := 0; index < writers; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			errors <- store.Save(State{Key: "KEY-" + strconv.Itoa(index), Status: "active"})
		}(index)
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent Save() error = %v", err)
		}
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after concurrent saves error = %v", err)
	}
	if state.Key == "" {
		t.Fatal("Load() after concurrent saves returned an empty state")
	}
}
