package entitlements

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"testing"
	"time"
)

func TestClientReleaseUsesProofAndClearsLeaseOnlyAfterSuccess(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	raw := signer.signLease(t, identity.ID, "lic-release-1", "nonce-release-1", nil)
	if _, err := store.AdvanceTrustedTime(signer.now); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}

	transport := mockControlPlaneTransport{
		release: func(_ context.Context, request ReleaseRequest) error {
			if request.LicenseID != "lic-release-1" || request.InstallationID != identity.ID || request.CurrentNonce != "nonce-release-1" {
				t.Fatalf("unexpected release request: %+v", request)
			}
			if err := VerifyReleaseProof(request, ed25519.PublicKey(identity.PublicKey)); err != nil {
				t.Fatalf("invalid release proof: %v", err)
			}
			tampered := request
			tampered.CurrentNonce = "nonce-attacker"
			if err := VerifyReleaseProof(tampered, ed25519.PublicKey(identity.PublicKey)); !errors.Is(err, ErrInvalidReleaseProof) {
				t.Fatalf("tampered proof error = %v, want %v", err, ErrInvalidReleaseProof)
			}
			return nil
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
	if err := client.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadLease(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lease cache error = %v, want os.ErrNotExist", err)
	}
	reloaded, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ID != identity.ID {
		t.Fatalf("installation id changed: %q != %q", reloaded.ID, identity.ID)
	}
}

func TestClientReleaseKeepsLeaseOnServerFailure(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	raw := signer.signLease(t, identity.ID, "lic-release-2", "nonce-release-2", nil)
	if _, err := store.AdvanceTrustedTime(signer.now); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}

	transport := mockControlPlaneTransport{
		release: func(context.Context, ReleaseRequest) error {
			return errors.New("server unavailable")
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
	if err := client.Release(context.Background()); err == nil {
		t.Fatal("release unexpectedly succeeded")
	}
	if _, err := store.LoadLease(); err != nil {
		t.Fatalf("lease was removed after failed release: %v", err)
	}
}
