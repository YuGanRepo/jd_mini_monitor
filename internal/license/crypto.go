package license

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

// EmbeddedPublicKey is the PKIX/SPKI-DER ECDSA P-256 public key of the license
// signer (the jd-chrome-plugin server's key pair). It is populated by init() in
// embedded_key.go. When nil the package runs in "dev mode" and accepts any
// signature — useful for local testing without the server.
var EmbeddedPublicKey []byte

// ParsePublicKeyDER returns an *ecdsa.PublicKey from a PKIX/SPKI DER-encoded
// ECDSA P-256 public key (the same base64 the server's /api/public-key returns,
// decoded to DER).
func ParsePublicKeyDER(der []byte) (*ecdsa.PublicKey, error) {
	raw, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	key, ok := raw.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ECDSA public key, got %T", raw)
	}
	if key.Curve != elliptic.P256() {
		return nil, errors.New("public key is not on P-256 curve")
	}
	return key, nil
}

// VerifySignature verifies an IEEE-P1363 (r||s, 64-byte) ECDSA P-256 / SHA-256
// signature over the canonical token string against EmbeddedPublicKey. This is
// the exact scheme used by the server (crypto.sign(..., dsaEncoding:
// 'ieee-p1363')) and the extension (WebCrypto ECDSA verify). Dev mode
// (EmbeddedPublicKey == nil) accepts any well-formed base64 signature.
func VerifySignature(payload TokenPayload, sigBase64 string) error {
	sig, err := base64.StdEncoding.DecodeString(sigBase64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	// Dev mode: no embedded key → accept (local testing without the server).
	if len(EmbeddedPublicKey) == 0 {
		return nil
	}

	if len(sig) != 64 {
		return fmt.Errorf("expected 64-byte IEEE-P1363 signature, got %d bytes", len(sig))
	}

	pub, err := ParsePublicKeyDER(EmbeddedPublicKey)
	if err != nil {
		return fmt.Errorf("embedded public key: %w", err)
	}

	digest := sha256.Sum256([]byte(CanonicalToken(payload)))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return errors.New("signature does not match token payload")
	}
	return nil
}
