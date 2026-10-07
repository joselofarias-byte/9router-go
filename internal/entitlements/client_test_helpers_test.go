package entitlements

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	json "encoding/json/v2"
	"testing"
	"time"
)

type mockControlPlaneTransport struct {
	activate func(context.Context, ActivationRequest) (LeaseResponse, error)
	renew    func(context.Context, RenewalRequest) (LeaseResponse, error)
	release  func(context.Context, ReleaseRequest) error
}

func (m mockControlPlaneTransport) Activate(ctx context.Context, request ActivationRequest) (LeaseResponse, error) {
	return m.activate(ctx, request)
}

func (m mockControlPlaneTransport) Renew(ctx context.Context, request RenewalRequest) (LeaseResponse, error) {
	return m.renew(ctx, request)
}

func (m mockControlPlaneTransport) Release(ctx context.Context, request ReleaseRequest) error {
	if m.release == nil {
		return nil
	}
	return m.release(ctx, request)
}

type clientTestSigner struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
	keyID      string
	now        time.Time
	build      BuildIdentity
}

func newClientTestSigner(t *testing.T) clientTestSigner {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)
	return clientTestSigner{
		publicKey:  publicKey,
		privateKey: privateKey,
		keyID:      "test-key-1",
		now:        now,
		build: BuildIdentity{
			Channel:         "beta",
			ID:              "beta-test-1",
			ProCapableUntil: now.Add(30 * 24 * time.Hour),
		},
	}
}

func (s clientTestSigner) keys() KeyRing {
	return KeyRing{s.keyID: append(ed25519.PublicKey(nil), s.publicKey...)}
}

func (s clientTestSigner) signLease(t *testing.T, installationID, licenseID, nonce string, mutate func(*Lease)) []byte {
	t.Helper()
	lease := &Lease{
		Version:            CurrentLeaseVersion,
		KeyID:              s.keyID,
		LicenseID:          licenseID,
		InstallationID:     installationID,
		Plan:               PlanBetaPro,
		EntitlementVersion: 1,
		Entitlements:       []Capability{CapabilityAdvancedRouting},
		IssuedAt:           s.now.Add(-time.Minute),
		ExpiresAt:          s.now.Add(7 * 24 * time.Hour),
		GraceUntil:         s.now.Add(10 * 24 * time.Hour),
		BuildChannel:       s.build.Channel,
		BuildID:            s.build.ID,
		Nonce:              nonce,
	}
	if mutate != nil {
		mutate(lease)
	}
	payload, err := canonicalLeasePayload(lease)
	if err != nil {
		t.Fatal(err)
	}
	lease.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.privateKey, payload))
	raw, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
