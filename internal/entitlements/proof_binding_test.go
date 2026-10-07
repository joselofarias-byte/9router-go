package entitlements

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func TestVerifyActivationProofBindsCodeDigestIdentityAndBuild(t *testing.T) {
	request, publicKey := activationProofFixture(t)
	raw, err := canonicalActivationProofPayload(request)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(request.ActivationCode)) {
		t.Fatal("activation proof contains the raw activation code")
	}
	if !bytes.Contains(raw, []byte(activationCodeHash(request.ActivationCode))) {
		t.Fatal("activation proof missing activation code digest")
	}
	if err := VerifyActivationProof(request, publicKey); err != nil {
		t.Fatal(err)
	}

	otherKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherEncoded, err := encodeInstallationPublicKey(otherKey)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*ActivationRequest)
	}{
		{name: "activation code", mutate: func(r *ActivationRequest) { r.ActivationCode += "-other" }},
		{name: "installation id", mutate: func(r *ActivationRequest) { r.InstallationID = installationIDFromPublicKey(otherKey) }},
		{name: "installation public key", mutate: func(r *ActivationRequest) { r.InstallationPublicKey = otherEncoded }},
		{name: "platform", mutate: func(r *ActivationRequest) { r.Platform = "windows" }},
		{name: "arch", mutate: func(r *ActivationRequest) { r.Arch = "arm64" }},
		{name: "app version", mutate: func(r *ActivationRequest) { r.AppVersion = "9.9.9" }},
		{name: "build channel", mutate: func(r *ActivationRequest) { r.BuildChannel = "stable" }},
		{name: "build id", mutate: func(r *ActivationRequest) { r.BuildID = "other-build" }},
		{name: "proof version", mutate: func(r *ActivationRequest) { r.ProofVersion = 0 }},
		{name: "proof signature", mutate: func(r *ActivationRequest) { r.ProofSignature = otherEncoded }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := request
			tt.mutate(&tampered)
			if err := VerifyActivationProof(tampered, publicKey); !errors.Is(err, ErrInvalidActivationProof) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidActivationProof)
			}
		})
	}
}

func TestVerifyActivationProofRejectsContradictoryIdentity(t *testing.T) {
	signerPub, signerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signerEncoded, err := encodeInstallationPublicKey(signerPub)
	if err != nil {
		t.Fatal(err)
	}
	otherEncoded, err := encodeInstallationPublicKey(otherPub)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		request   ActivationRequest
		publicKey ed25519.PublicKey
		wantErr   bool
	}{
		{
			name: "key-backed id of another key",
			request: ActivationRequest{
				ActivationCode:        "beta-invite-code",
				InstallationID:        installationIDFromPublicKey(otherPub),
				InstallationPublicKey: signerEncoded,
				Platform:              "linux",
				Arch:                  "amd64",
				BuildChannel:          "beta",
				BuildID:               "beta-20261006.1",
				ProofVersion:          ActivationProofVersion,
			},
			publicKey: signerPub,
			wantErr:   true,
		},
		{
			name: "public key field does not match verification key",
			request: ActivationRequest{
				ActivationCode:        "beta-invite-code",
				InstallationID:        installationIDFromPublicKey(signerPub),
				InstallationPublicKey: otherEncoded,
				Platform:              "linux",
				Arch:                  "amd64",
				BuildChannel:          "beta",
				BuildID:               "beta-20261006.1",
				ProofVersion:          ActivationProofVersion,
			},
			publicKey: signerPub,
			wantErr:   true,
		},
		{
			name: "legacy uuid remains valid for its proof key",
			request: ActivationRequest{
				ActivationCode:        "beta-invite-code",
				InstallationID:        "11111111-2222-4333-8444-555555555555",
				InstallationPublicKey: signerEncoded,
				Platform:              "linux",
				Arch:                  "amd64",
				BuildChannel:          "beta",
				BuildID:               "beta-20261006.1",
				ProofVersion:          ActivationProofVersion,
			},
			publicKey: signerPub,
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature, err := signActivationProof(signerPriv, tt.request)
			if err != nil {
				t.Fatal(err)
			}
			tt.request.ProofSignature = signature
			err = VerifyActivationProof(tt.request, tt.publicKey)
			if tt.wantErr && !errors.Is(err, ErrInvalidActivationProof) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidActivationProof)
			}
			if !tt.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVerifyRenewalProofBindsLicenseInstallationNonceAndBuild(t *testing.T) {
	request, publicKey := renewalProofFixture(t)
	if err := VerifyRenewalProof(request, publicKey); err != nil {
		t.Fatal(err)
	}

	otherKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*RenewalRequest)
	}{
		{name: "license id", mutate: func(r *RenewalRequest) { r.LicenseID = "other-license" }},
		{name: "installation id", mutate: func(r *RenewalRequest) { r.InstallationID = installationIDFromPublicKey(otherKey) }},
		{name: "nonce", mutate: func(r *RenewalRequest) { r.CurrentNonce = "other-nonce" }},
		{name: "platform", mutate: func(r *RenewalRequest) { r.Platform = "windows" }},
		{name: "arch", mutate: func(r *RenewalRequest) { r.Arch = "arm64" }},
		{name: "app version", mutate: func(r *RenewalRequest) { r.AppVersion = "9.9.9" }},
		{name: "build channel", mutate: func(r *RenewalRequest) { r.BuildChannel = "stable" }},
		{name: "build id", mutate: func(r *RenewalRequest) { r.BuildID = "other-build" }},
		{name: "proof version", mutate: func(r *RenewalRequest) { r.ProofVersion = 0 }},
		{name: "proof signature", mutate: func(r *RenewalRequest) { r.ProofSignature = "not-a-signature" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := request
			tt.mutate(&tampered)
			if err := VerifyRenewalProof(tampered, publicKey); !errors.Is(err, ErrInvalidRenewalProof) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidRenewalProof)
			}
		})
	}
}

func TestVerifyRenewalProofRejectsUnrelatedKeyForKeyBackedInstallation(t *testing.T) {
	signerPub, signerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request := RenewalRequest{
		LicenseID:      "lic-copied",
		InstallationID: installationIDFromPublicKey(otherPub),
		CurrentNonce:   "nonce-copied",
		Platform:       "linux",
		Arch:           "amd64",
		AppVersion:     "1.9.6",
		BuildChannel:   "beta",
		BuildID:        "beta-20261006.1",
		ProofVersion:   RenewalProofVersion,
	}
	signature, err := signRenewalProof(signerPriv, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ProofSignature = signature
	if err := VerifyRenewalProof(request, signerPub); !errors.Is(err, ErrInvalidRenewalProof) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidRenewalProof)
	}
}

func TestVerifyReleaseProofBindsActionLicenseInstallationAndNonce(t *testing.T) {
	request, publicKey := releaseProofFixture(t)
	raw, err := canonicalReleaseProofPayload(request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"action":"release"`)) {
		t.Fatalf("release payload = %s", raw)
	}
	if err := VerifyReleaseProof(request, publicKey); err != nil {
		t.Fatal(err)
	}

	otherKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*ReleaseRequest)
	}{
		{name: "license id", mutate: func(r *ReleaseRequest) { r.LicenseID = "other-license" }},
		{name: "installation id", mutate: func(r *ReleaseRequest) { r.InstallationID = installationIDFromPublicKey(otherKey) }},
		{name: "nonce", mutate: func(r *ReleaseRequest) { r.CurrentNonce = "other-nonce" }},
		{name: "proof version", mutate: func(r *ReleaseRequest) { r.ProofVersion = 0 }},
		{name: "proof signature", mutate: func(r *ReleaseRequest) { r.ProofSignature = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := request
			tt.mutate(&tampered)
			if err := VerifyReleaseProof(tampered, publicKey); !errors.Is(err, ErrInvalidReleaseProof) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidReleaseProof)
			}
		})
	}
}

func TestVerifyReleaseProofRejectsUnrelatedKeyForKeyBackedInstallation(t *testing.T) {
	signerPub, signerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request := ReleaseRequest{
		LicenseID:      "lic-copied",
		InstallationID: installationIDFromPublicKey(otherPub),
		CurrentNonce:   "nonce-copied",
		ProofVersion:   ReleaseProofVersion,
	}
	signature, err := signReleaseProof(signerPriv, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ProofSignature = signature
	if err := VerifyReleaseProof(request, signerPub); !errors.Is(err, ErrInvalidReleaseProof) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidReleaseProof)
	}
}

func TestActivationRenewalAndReleaseProofsAreDomainSeparated(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeInstallationPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	installationID := installationIDFromPublicKey(publicKey)

	activation := ActivationRequest{
		ActivationCode:        "beta-invite-code",
		InstallationID:        installationID,
		InstallationPublicKey: encoded,
		Platform:              "linux",
		Arch:                  "amd64",
		AppVersion:            "1.9.6",
		BuildChannel:          "beta",
		BuildID:               "beta-20261006.1",
		ProofVersion:          ActivationProofVersion,
	}
	activation.ProofSignature, err = signActivationProof(privateKey, activation)
	if err != nil {
		t.Fatal(err)
	}

	renewal := RenewalRequest{
		LicenseID:      "lic-domain",
		InstallationID: installationID,
		CurrentNonce:   "nonce-domain",
		Platform:       "linux",
		Arch:           "amd64",
		AppVersion:     "1.9.6",
		BuildChannel:   "beta",
		BuildID:        "beta-20261006.1",
		ProofVersion:   RenewalProofVersion,
	}
	renewal.ProofSignature, err = signRenewalProof(privateKey, renewal)
	if err != nil {
		t.Fatal(err)
	}

	release := ReleaseRequest{
		LicenseID:      renewal.LicenseID,
		InstallationID: installationID,
		CurrentNonce:   renewal.CurrentNonce,
		ProofVersion:   ReleaseProofVersion,
	}
	release.ProofSignature, err = signReleaseProof(privateKey, release)
	if err != nil {
		t.Fatal(err)
	}

	asRenewal := renewal
	asRenewal.ProofSignature = release.ProofSignature
	if err := VerifyRenewalProof(asRenewal, publicKey); !errors.Is(err, ErrInvalidRenewalProof) {
		t.Fatalf("release proof accepted as renewal: %v", err)
	}

	asRelease := release
	asRelease.ProofSignature = renewal.ProofSignature
	if err := VerifyReleaseProof(asRelease, publicKey); !errors.Is(err, ErrInvalidReleaseProof) {
		t.Fatalf("renewal proof accepted as release: %v", err)
	}

	activationAsRenewal := renewal
	activationAsRenewal.ProofSignature = activation.ProofSignature
	if err := VerifyRenewalProof(activationAsRenewal, publicKey); !errors.Is(err, ErrInvalidRenewalProof) {
		t.Fatalf("activation proof accepted as renewal: %v", err)
	}
}

func activationProofFixture(t *testing.T) (ActivationRequest, ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeInstallationPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := ActivationRequest{
		ActivationCode:        "beta-invite-code",
		InstallationID:        installationIDFromPublicKey(publicKey),
		InstallationPublicKey: encoded,
		Platform:              "linux",
		Arch:                  "amd64",
		AppVersion:            "1.9.6",
		BuildChannel:          "beta",
		BuildID:               "beta-20261006.1",
		ProofVersion:          ActivationProofVersion,
	}
	request.ProofSignature, err = signActivationProof(privateKey, request)
	if err != nil {
		t.Fatal(err)
	}
	return request, publicKey
}

func renewalProofFixture(t *testing.T) (RenewalRequest, ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request := RenewalRequest{
		LicenseID:      "lic-renew-proof",
		InstallationID: installationIDFromPublicKey(publicKey),
		CurrentNonce:   "nonce-renew-proof",
		Platform:       "linux",
		Arch:           "amd64",
		AppVersion:     "1.9.6",
		BuildChannel:   "beta",
		BuildID:        "beta-20261006.1",
		ProofVersion:   RenewalProofVersion,
	}
	request.ProofSignature, err = signRenewalProof(privateKey, request)
	if err != nil {
		t.Fatal(err)
	}
	return request, publicKey
}

func releaseProofFixture(t *testing.T) (ReleaseRequest, ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request := ReleaseRequest{
		LicenseID:      "lic-release-proof",
		InstallationID: installationIDFromPublicKey(publicKey),
		CurrentNonce:   "nonce-release-proof",
		ProofVersion:   ReleaseProofVersion,
	}
	request.ProofSignature, err = signReleaseProof(privateKey, request)
	if err != nil {
		t.Fatal(err)
	}
	return request, publicKey
}
