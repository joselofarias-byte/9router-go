package entitlements

import (
	"crypto/ed25519"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"
)

const CurrentLicenseVersion = 1

var (
	ErrMalformedLicense = errors.New("malformed license")
	ErrInvalidSignature = errors.New("invalid license signature")
	ErrExpiredLicense   = errors.New("expired license")
	ErrFutureLicense    = errors.New("license issued in the future")
	ErrInvalidPublicKey = errors.New("invalid entitlement public key")
)

// License is the verified beta/commercial entitlement envelope. Signature is
// encoded separately from the canonical payload so modifying any signed field
// invalidates verification.
type License struct {
	Version   int          `json:"version"`
	LicenseID string       `json:"license_id"`
	Channel   string       `json:"channel"`
	IssuedAt  time.Time    `json:"issued_at"`
	ExpiresAt time.Time    `json:"expires_at"`
	Subject   string       `json:"subject,omitempty"`
	Features  []Capability `json:"features"`
	Signature string       `json:"signature"`
}

type signingPayload struct {
	Version   int          `json:"version"`
	LicenseID string       `json:"license_id"`
	Channel   string       `json:"channel"`
	IssuedAt  time.Time    `json:"issued_at"`
	ExpiresAt time.Time    `json:"expires_at"`
	Subject   string       `json:"subject,omitempty"`
	Features  []Capability `json:"features"`
}

func (l *License) payload() signingPayload {
	if l == nil {
		return signingPayload{}
	}
	return signingPayload{
		Version:   l.Version,
		LicenseID: l.LicenseID,
		Channel:   l.Channel,
		IssuedAt:  l.IssuedAt.UTC(),
		ExpiresAt: l.ExpiresAt.UTC(),
		Subject:   l.Subject,
		Features:  append([]Capability(nil), l.Features...),
	}
}

func canonicalPayload(l *License) ([]byte, error) {
	payload, err := json.Marshal(l.payload())
	if err != nil {
		return nil, fmt.Errorf("entitlements.canonicalPayload: %w", err)
	}
	return payload, nil
}

// ParsePublicKey decodes the base64 Ed25519 public key embedded/configured by a
// beta build. The corresponding private key must never be shipped.
func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidPublicKey, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// VerifySignedLicense parses and verifies an Ed25519-signed license at a caller-
// supplied clock instant. Passing now explicitly keeps expiry tests deterministic.
func VerifySignedLicense(raw []byte, publicKey ed25519.PublicKey, now time.Time) (*License, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidPublicKey
	}

	var license License
	if err := json.Unmarshal(raw, &license); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedLicense, err)
	}
	if err := validateLicenseShape(&license); err != nil {
		return nil, err
	}

	signature, err := base64.StdEncoding.DecodeString(license.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, ErrInvalidSignature
	}
	payload, err := canonicalPayload(&license)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return nil, ErrInvalidSignature
	}

	now = now.UTC()
	if license.IssuedAt.After(now.Add(5 * time.Minute)) {
		return nil, ErrFutureLicense
	}
	if !now.Before(license.ExpiresAt) {
		return nil, ErrExpiredLicense
	}

	license.Features = dedupeCapabilities(license.Features)
	return &license, nil
}

func validateLicenseShape(license *License) error {
	if license == nil ||
		license.Version != CurrentLicenseVersion ||
		strings.TrimSpace(license.LicenseID) == "" ||
		strings.TrimSpace(license.Channel) == "" ||
		license.IssuedAt.IsZero() ||
		license.ExpiresAt.IsZero() ||
		!license.ExpiresAt.After(license.IssuedAt) ||
		strings.TrimSpace(license.Signature) == "" {
		return ErrMalformedLicense
	}
	return nil
}

func dedupeCapabilities(features []Capability) []Capability {
	if len(features) < 2 {
		return append([]Capability(nil), features...)
	}
	seen := make(map[Capability]struct{}, len(features))
	out := make([]Capability, 0, len(features))
	for _, feature := range features {
		if feature == "" {
			continue
		}
		if _, ok := seen[feature]; ok {
			continue
		}
		seen[feature] = struct{}{}
		out = append(out, feature)
	}
	return out
}
