package entitlements

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"
)

func TestPrivateWorkerCrossLanguageLeaseVector(t *testing.T) {
	publicRaw, err := base64.StdEncoding.DecodeString("A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg=")
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if len(publicRaw) != ed25519.PublicKeySize {
		t.Fatalf("public key len = %d, want %d", len(publicRaw), ed25519.PublicKeySize)
	}

	raw := []byte(`{"version":1,"kid":"crosslang-test-1","license_id":"lic-crosslang-001","installation_id":"12345678-1234-4234-8234-1234567890ab","plan":"beta_pro","entitlement_version":7,"entitlements":["fabric.advanced_routing","policy.budget"],"issued_at":"2026-10-06T14:30:00.12Z","expires_at":"2026-10-13T14:30:00.12Z","grace_until":"2026-10-16T14:30:00.12Z","build_channel":"beta","build_id":"beta-crosslang-1","nonce":"nonce-crosslang-001","signature":"X6K7a0T/ZOb1sXeuzjixV3C8heajRsUf5pDK06vMDNNvLYMJ+RCe1HzsUcXEcOqOmsdceJ9mbqMXRJ69HjOlAA=="}`)

	evaluation, err := VerifySignedLease(raw, KeyRing{
		"crosslang-test-1": ed25519.PublicKey(publicRaw),
	}, VerificationContext{
		Now:            time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		InstallationID: "12345678-1234-4234-8234-1234567890ab",
		Build: BuildIdentity{
			Channel:         "beta",
			ID:              "beta-crosslang-1",
			ProCapableUntil: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("VerifySignedLease() error = %v", err)
	}
	if evaluation.State != LeaseStateActive {
		t.Fatalf("state = %q, want %q", evaluation.State, LeaseStateActive)
	}
	if evaluation.Lease.LicenseID != "lic-crosslang-001" {
		t.Fatalf("license id = %q", evaluation.Lease.LicenseID)
	}
	if evaluation.Lease.EntitlementVersion != 7 {
		t.Fatalf("entitlement version = %d", evaluation.Lease.EntitlementVersion)
	}
	if len(evaluation.Lease.Entitlements) != 2 {
		t.Fatalf("entitlements len = %d, want 2", len(evaluation.Lease.Entitlements))
	}
}
