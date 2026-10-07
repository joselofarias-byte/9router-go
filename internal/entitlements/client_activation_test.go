package entitlements

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientActivationPersistsOnlyVerifiedLease(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	const activationCode = "beta-code-for-test"

	var returnedLease []byte
	transport := mockControlPlaneTransport{
		activate: func(_ context.Context, request ActivationRequest) (LeaseResponse, error) {
			if request.ActivationCode != activationCode {
				t.Fatalf("activation code = %q", request.ActivationCode)
			}
			if request.InstallationID == "" {
				t.Fatal("missing installation id")
			}
			if !strings.HasPrefix(request.InstallationID, installationIDPrefix) {
				t.Fatalf("installation id %q is not key-backed", request.InstallationID)
			}
			publicKey, err := base64.StdEncoding.DecodeString(request.InstallationPublicKey)
			if err != nil || len(publicKey) != ed25519.PublicKeySize {
				t.Fatalf("invalid installation public key: %q", request.InstallationPublicKey)
			}
			if request.InstallationID != installationIDFromPublicKey(ed25519.PublicKey(publicKey)) {
				t.Fatal("installation id does not match activation public key")
			}
			if request.ProofVersion != ActivationProofVersion {
				t.Fatalf("proof version = %d", request.ProofVersion)
			}
			if request.Platform != "android" || request.Arch != "arm64" || request.AppVersion != "test" {
				t.Fatalf("activation runtime = %s/%s/%s", request.Platform, request.Arch, request.AppVersion)
			}
			if request.BuildChannel != signer.build.Channel || request.BuildID != signer.build.ID {
				t.Fatalf("activation build = %s/%s", request.BuildChannel, request.BuildID)
			}
			if err := VerifyActivationProof(request, ed25519.PublicKey(publicKey)); err != nil {
				t.Fatalf("activation proof invalid: %v", err)
			}
			tampered := request
			tampered.ActivationCode = "different-code"
			if err := VerifyActivationProof(tampered, ed25519.PublicKey(publicKey)); !errors.Is(err, ErrInvalidActivationProof) {
				t.Fatalf("tampered activation proof error = %v, want %v", err, ErrInvalidActivationProof)
			}
			returnedLease = signer.signLease(t, request.InstallationID, "lic-activation-1", "nonce-a1", nil)
			return LeaseResponse{
				Lease:               returnedLease,
				ServerTime:          signer.now.Add(2 * time.Minute),
				RenewalAfterSeconds: 3600,
			}, nil
		},
		renew: func(context.Context, RenewalRequest) (LeaseResponse, error) {
			t.Fatal("unexpected renew")
			return LeaseResponse{}, nil
		},
	}

	client, err := NewClient(ClientOptions{
		Store:      store,
		Transport:  transport,
		Keys:       signer.keys(),
		Build:      signer.build,
		Platform:   "android",
		Arch:       "arm64",
		AppVersion: "test",
		Now:        func() time.Time { return signer.now },
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Activate(context.Background(), activationCode)
	if err != nil {
		t.Fatal(err)
	}
	if result.Evaluation.Lease.LicenseID != "lic-activation-1" {
		t.Fatalf("license id = %q", result.Evaluation.Lease.LicenseID)
	}
	if result.RenewalAfter != time.Hour {
		t.Fatalf("renewal after = %s", result.RenewalAfter)
	}

	cached, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(returnedLease) {
		t.Fatal("persisted lease differs from verified response")
	}

	trusted, err := store.LastTrustedTime()
	if err != nil {
		t.Fatal(err)
	}
	if !trusted.Equal(signer.now.Add(2 * time.Minute)) {
		t.Fatalf("trusted time = %s", trusted)
	}

	err = filepath.Walk(store.Dir(), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), activationCode) {
			t.Fatalf("activation code persisted in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientActivationRejectsInvalidLeaseWithoutPersistingIt(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())

	transport := mockControlPlaneTransport{
		activate: func(_ context.Context, request ActivationRequest) (LeaseResponse, error) {
			raw := signer.signLease(t, request.InstallationID, "lic-activation-2", "nonce-a2", nil)
			raw[len(raw)-2] ^= 1
			return LeaseResponse{Lease: raw, ServerTime: signer.now}, nil
		},
		renew: func(context.Context, RenewalRequest) (LeaseResponse, error) {
			return LeaseResponse{}, errors.New("unexpected renew")
		},
	}

	client, err := NewClient(ClientOptions{
		Store:     store,
		Transport: transport,
		Keys:      signer.keys(),
		Build:     signer.build,
		Now:       func() time.Time { return signer.now },
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Activate(context.Background(), "test-code"); err == nil {
		t.Fatal("tampered activation lease unexpectedly accepted")
	}
	if _, err := store.LoadLease(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lease cache error = %v, want os.ErrNotExist", err)
	}
}

func TestClientActivationRejectsWrongInstallation(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())

	transport := mockControlPlaneTransport{
		activate: func(_ context.Context, request ActivationRequest) (LeaseResponse, error) {
			raw := signer.signLease(t, "99999999-2222-4333-8444-555555555555", "lic-wrong-install", "nonce-wrong", nil)
			return LeaseResponse{Lease: raw, ServerTime: signer.now}, nil
		},
		renew: func(context.Context, RenewalRequest) (LeaseResponse, error) {
			return LeaseResponse{}, errors.New("unexpected renew")
		},
	}

	client, err := NewClient(ClientOptions{
		Store:     store,
		Transport: transport,
		Keys:      signer.keys(),
		Build:     signer.build,
		Now:       func() time.Time { return signer.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Activate(context.Background(), "test-code")
	if !errors.Is(err, ErrWrongInstallation) {
		t.Fatalf("error = %v, want %v", err, ErrWrongInstallation)
	}
	if _, err := store.LoadLease(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lease cache error = %v, want os.ErrNotExist", err)
	}
}
