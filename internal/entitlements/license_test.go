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

func signedFixture(t *testing.T, mutate func(*License)) ([]byte, ed25519.PublicKey, time.Time) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	now := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	license := &License{
		Version:   CurrentLicenseVersion,
		LicenseID: "beta-00127",
		Channel:   "beta",
		IssuedAt:  now.Add(-time.Hour),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
		Subject:   "tester-127",
		Features: []Capability{
			CapabilityAdvancedRouting,
			CapabilityMultiAccountPolicy,
			CapabilityAdvancedRouting,
		},
	}
	if mutate != nil {
		mutate(license)
	}

	payload, err := canonicalPayload(license)
	if err != nil {
		t.Fatalf("canonicalPayload() error = %v", err)
	}
	license.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))

	raw, err := json.Marshal(license)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return raw, publicKey, now
}

func TestVerifySignedLicense(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*License)
		now     func(time.Time) time.Time
		wantErr error
	}{
		{
			name: "valid beta license",
		},
		{
			name: "expired",
			now: func(now time.Time) time.Time {
				return now.Add(31 * 24 * time.Hour)
			},
			wantErr: ErrExpiredLicense,
		},
		{
			name: "issued too far in future",
			mutate: func(license *License) {
				license.IssuedAt = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
				license.ExpiresAt = license.IssuedAt.Add(24 * time.Hour)
			},
			wantErr: ErrFutureLicense,
		},
		{
			name: "bad version",
			mutate: func(license *License) {
				license.Version = 99
			},
			wantErr: ErrMalformedLicense,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, publicKey, now := signedFixture(t, tt.mutate)
			if tt.now != nil {
				now = tt.now(now)
			}

			got, err := VerifySignedLicense(raw, publicKey, now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifySignedLicense() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.LicenseID != "beta-00127" {
				t.Errorf("LicenseID = %q, want beta-00127", got.LicenseID)
			}
			if len(got.Features) != 2 {
				t.Errorf("Features len = %d, want 2 after dedupe", len(got.Features))
			}
		})
	}
}

func TestVerifySignedLicenseRejectsTampering(t *testing.T) {
	raw, publicKey, now := signedFixture(t, nil)

	var license License
	if err := json.Unmarshal(raw, &license); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	license.Features = append(license.Features, CapabilityBudgetPolicy)
	tampered, err := json.Marshal(license)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	_, err = VerifySignedLicense(tampered, publicKey, now)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifySignedLicense() error = %v, want %v", err, ErrInvalidSignature)
	}
}

func TestParsePublicKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey)

	got, err := ParsePublicKey(encoded)
	if err != nil {
		t.Fatalf("ParsePublicKey() error = %v", err)
	}
	if string(got) != string(publicKey) {
		t.Fatal("ParsePublicKey() returned different key")
	}
}

func TestProviders(t *testing.T) {
	var community Provider = CommunityProvider{}
	if community.Enabled(CapabilityAdvancedRouting) {
		t.Fatal("community provider enabled Pro capability")
	}
	if community.Status().Mode != "community" {
		t.Fatalf("community mode = %q", community.Status().Mode)
	}

	license := &License{
		LicenseID: "beta-x",
		Channel:   "beta",
		ExpiresAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Features:  []Capability{CapabilityAdvancedRouting},
	}
	var licensed Provider = NewLicenseProvider(license)
	if !licensed.Enabled(CapabilityAdvancedRouting) {
		t.Fatal("licensed provider did not enable feature")
	}
	if licensed.Enabled(CapabilityBudgetPolicy) {
		t.Fatal("licensed provider enabled absent feature")
	}
}

func TestLicenseProviderExpiresWithoutReload(t *testing.T) {
	raw, publicKey, now := signedFixture(t, nil)
	license, err := VerifySignedLicense(raw, publicKey, now)
	if err != nil {
		t.Fatalf("VerifySignedLicense() error = %v", err)
	}
	provider := NewLicenseProvider(license)
	current := now
	provider.now = func() time.Time { return current }

	tests := []struct {
		name    string
		at      time.Time
		enabled bool
		mode    string
	}{
		{"before expiry", license.ExpiresAt.Add(-time.Nanosecond), true, "licensed"},
		{"exact expiry", license.ExpiresAt, false, "community"},
		{"after expiry", license.ExpiresAt.Add(time.Hour), false, "community"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current = tt.at
			if got := provider.Enabled(CapabilityAdvancedRouting); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
			status := provider.Status()
			if status.Mode != tt.mode {
				t.Errorf("Status().Mode = %q, want %q", status.Mode, tt.mode)
			}
			if !tt.enabled && (len(status.Features) != 0 || status.LicenseID != "" || status.ExpiresAt != nil) {
				t.Errorf("expired provider exposed licensed status: %+v", status)
			}
		})
	}
}

func TestLicenseProviderSnapshotsVerifiedInput(t *testing.T) {
	raw, publicKey, now := signedFixture(t, nil)
	license, err := VerifySignedLicense(raw, publicKey, now)
	if err != nil {
		t.Fatalf("VerifySignedLicense() error = %v", err)
	}
	expiry := license.ExpiresAt
	provider := NewLicenseProvider(license)
	provider.now = func() time.Time { return now }

	license.ExpiresAt = expiry.Add(time.Hour)
	license.Features[0] = CapabilityBudgetPolicy
	status := provider.Status()
	if status.ExpiresAt == nil || !status.ExpiresAt.Equal(expiry) {
		t.Fatal("caller mutation changed provider expiry")
	}
	if status.Features[0] != CapabilityAdvancedRouting || provider.Enabled(CapabilityBudgetPolicy) {
		t.Fatal("caller mutation changed verified capabilities")
	}
	now = expiry
	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("caller mutation extended provider validity")
	}
}
