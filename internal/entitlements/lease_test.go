package entitlements

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"testing"
	"time"
)

func signedLeaseFixture(t *testing.T, mutate func(*Lease)) ([]byte, KeyRing, VerificationContext, *Lease) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	now := time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC)
	lease := &Lease{
		Version:            CurrentLeaseVersion,
		KeyID:              "2026-10-a",
		LicenseID:          "lic-beta-00127",
		InstallationID:     "install-android-a",
		Plan:               PlanBetaPro,
		EntitlementVersion: 3,
		Entitlements: []Capability{
			CapabilityAdvancedRouting,
			CapabilityMultiAccountPolicy,
			CapabilityAdvancedRouting,
		},
		IssuedAt:     now.Add(-time.Hour),
		ExpiresAt:    now.Add(7 * 24 * time.Hour),
		GraceUntil:  now.Add(10 * 24 * time.Hour),
		BuildChannel: "beta",
		BuildID:       "beta-20261005.1",
		Nonce:         "nonce-001",
	}
	if mutate != nil {
		mutate(lease)
	}

	payload, err := canonicalLeasePayload(lease)
	if err != nil {
		t.Fatalf("canonicalLeasePayload() error = %v", err)
	}
	lease.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))

	raw, err := json.Marshal(lease)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	ctx := VerificationContext{
		Now:            now,
		InstallationID: lease.InstallationID,
		Build: BuildIdentity{
			Channel:         lease.BuildChannel,
			ID:              lease.BuildID,
			ProCapableUntil: now.Add(30 * 24 * time.Hour),
		},
	}
	return raw, KeyRing{lease.KeyID: publicKey}, ctx, lease
}

func TestVerifySignedLeaseActiveAndDedupes(t *testing.T) {
	raw, keys, ctx, _ := signedLeaseFixture(t, nil)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatalf("VerifySignedLease() error = %v", err)
	}
	if evaluation.State != LeaseStateActive {
		t.Fatalf("state = %q, want %q", evaluation.State, LeaseStateActive)
	}
	if len(evaluation.Lease.Entitlements) != 2 {
		t.Fatalf("entitlements len = %d, want 2", len(evaluation.Lease.Entitlements))
	}
}

func TestVerifySignedLeaseRejectsTampering(t *testing.T) {
	raw, keys, ctx, _ := signedLeaseFixture(t, nil)

	var lease Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	lease.Entitlements = append(lease.Entitlements, CapabilityBudgetPolicy)
	tampered, err := json.Marshal(lease)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if _, err := VerifySignedLease(tampered, keys, ctx); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifySignedLease() error = %v, want %v", err, ErrInvalidSignature)
	}
}

func TestVerifySignedLeaseGraceAndFinalExpiry(t *testing.T) {
	raw, keys, ctx, lease := signedLeaseFixture(t, nil)

	ctx.Now = lease.ExpiresAt.Add(time.Hour)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatalf("grace VerifySignedLease() error = %v", err)
	}
	if evaluation.State != LeaseStateGrace {
		t.Fatalf("state = %q, want %q", evaluation.State, LeaseStateGrace)
	}

	ctx.Now = lease.GraceUntil
	if _, err := VerifySignedLease(raw, keys, ctx); !errors.Is(err, ErrExpiredLease) {
		t.Fatalf("final expiry error = %v, want %v", err, ErrExpiredLease)
	}
}

func TestVerifySignedLeaseBindingAndClockFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*VerificationContext, KeyRing)
		wantErr error
	}{
		{
			name: "wrong installation",
			mutate: func(ctx *VerificationContext, _ KeyRing) {
				ctx.InstallationID = "install-linux-b"
			},
			wantErr: ErrWrongInstallation,
		},
		{
			name: "wrong channel",
			mutate: func(ctx *VerificationContext, _ KeyRing) {
				ctx.Build.Channel = "stable"
			},
			wantErr: ErrWrongBuildChannel,
		},
		{
			name: "wrong build id",
			mutate: func(ctx *VerificationContext, _ KeyRing) {
				ctx.Build.ID = "beta-other"
			},
			wantErr: ErrWrongBuildID,
		},
		{
			name: "unknown signing key",
			mutate: func(_ *VerificationContext, keys KeyRing) {
				delete(keys, "2026-10-a")
			},
			wantErr: ErrUnknownKey,
		},
		{
			name: "build hard expiry",
			mutate: func(ctx *VerificationContext, _ KeyRing) {
				ctx.Build.ProCapableUntil = ctx.Now
			},
			wantErr: ErrBuildExpired,
		},
		{
			name: "clock rollback",
			mutate: func(ctx *VerificationContext, _ KeyRing) {
				ctx.LastTrustedTime = ctx.Now.Add(10 * time.Minute)
			},
			wantErr: ErrClockRollback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, keys, ctx, _ := signedLeaseFixture(t, nil)
			tt.mutate(&ctx, keys)
			if _, err := VerifySignedLease(raw, keys, ctx); !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifySignedLease() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifySignedLeaseKeyRotation(t *testing.T) {
	raw, keys, ctx, lease := signedLeaseFixture(t, func(lease *Lease) {
		lease.KeyID = "2026-10-b"
	})

	oldPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keys["2026-09-a"] = oldPublic

	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatalf("VerifySignedLease() error = %v", err)
	}
	if evaluation.Lease.KeyID != lease.KeyID {
		t.Fatalf("kid = %q, want %q", evaluation.Lease.KeyID, lease.KeyID)
	}
}

func TestSameLicenseAcrossMixedInstallations(t *testing.T) {
	rawA, keysA, ctxA, leaseA := signedLeaseFixture(t, nil)
	if _, err := VerifySignedLease(rawA, keysA, ctxA); err != nil {
		t.Fatalf("install A VerifySignedLease() error = %v", err)
	}

	rawB, keysB, ctxB, leaseB := signedLeaseFixture(t, func(lease *Lease) {
		lease.InstallationID = "install-linux-b"
	})
	leaseB.LicenseID = leaseA.LicenseID
	if _, err := VerifySignedLease(rawB, keysB, ctxB); err != nil {
		t.Fatalf("install B VerifySignedLease() error = %v", err)
	}
	if leaseA.LicenseID != leaseB.LicenseID {
		t.Fatal("fixture licenses do not share identity")
	}
}

func TestLeaseProviderTransitionsWithoutReload(t *testing.T) {
	raw, keys, ctx, lease := signedLeaseFixture(t, nil)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatalf("VerifySignedLease() error = %v", err)
	}
	provider := NewLeaseProvider(evaluation)
	current := ctx.Now
	provider.now = func() time.Time { return current }

	current = lease.ExpiresAt.Add(-time.Nanosecond)
	if !provider.Enabled(CapabilityAdvancedRouting) || provider.Status().State != string(LeaseStateActive) {
		t.Fatal("provider did not remain active before lease expiry")
	}

	current = lease.ExpiresAt
	if !provider.Enabled(CapabilityAdvancedRouting) || provider.Status().State != string(LeaseStateGrace) {
		t.Fatal("provider did not enter grace at lease expiry")
	}

	current = lease.GraceUntil
	status := provider.Status()
	if provider.Enabled(CapabilityAdvancedRouting) || status.Mode != "community" {
		t.Fatalf("provider did not degrade to Community after grace: %+v", status)
	}
}

func TestParseKeyRing(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keys, err := ParseKeyRing(map[string]string{
		"2026-10-a": base64.StdEncoding.EncodeToString(publicKey),
	})
	if err != nil {
		t.Fatalf("ParseKeyRing() error = %v", err)
	}
	if got := keys["2026-10-a"]; string(got) != string(publicKey) {
		t.Fatal("ParseKeyRing() returned different public key")
	}
}
