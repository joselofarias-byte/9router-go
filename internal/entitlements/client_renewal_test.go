package entitlements

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClientRenewUsesVerifiedCachedIdentityAndPersistsRenewal(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	installationID := identity.ID
	initial := signer.signLease(t, installationID, "lic-renew-1", "nonce-old", nil)
	if err := store.SaveLease(initial); err != nil {
		t.Fatal(err)
	}

	var renewed []byte
	transport := mockControlPlaneTransport{
		activate: func(context.Context, ActivationRequest) (LeaseResponse, error) {
			return LeaseResponse{}, errors.New("unexpected activate")
		},
		renew: func(_ context.Context, request RenewalRequest) (LeaseResponse, error) {
			if request.LicenseID != "lic-renew-1" {
				t.Fatalf("renew license id = %q", request.LicenseID)
			}
			if request.CurrentNonce != "nonce-old" {
				t.Fatalf("renew nonce = %q", request.CurrentNonce)
			}
			if request.InstallationID != installationID {
				t.Fatalf("renew installation = %q", request.InstallationID)
			}
			if request.ProofVersion != RenewalProofVersion {
				t.Fatalf("proof version = %d", request.ProofVersion)
			}
			if err := VerifyRenewalProof(request, identity.PublicKey); err != nil {
				t.Fatalf("renewal proof rejected: %v", err)
			}
			tampered := request
			tampered.CurrentNonce = "nonce-attacker"
			if err := VerifyRenewalProof(tampered, identity.PublicKey); err == nil {
				t.Fatal("tampered renewal proof unexpectedly verified")
			}
			renewed = signer.signLease(t, installationID, "lic-renew-1", "nonce-new", func(lease *Lease) {
				lease.ExpiresAt = signer.now.Add(14 * 24 * time.Hour)
				lease.GraceUntil = signer.now.Add(17 * 24 * time.Hour)
			})
			return LeaseResponse{
				Lease:               renewed,
				ServerTime:          signer.now.Add(3 * time.Minute),
				RenewalAfterSeconds: 7200,
			}, nil
		},
	}

	client, err := NewClient(ClientOptions{
		Store:      store,
		Transport:  transport,
		Keys:       signer.keys(),
		Build:      signer.build,
		Platform:   "linux",
		Arch:       "amd64",
		AppVersion: "test",
		Now:        func() time.Time { return signer.now },
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Renew(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Evaluation.Lease.Nonce != "nonce-new" {
		t.Fatalf("renewed nonce = %q", result.Evaluation.Lease.Nonce)
	}
	if result.RenewalAfter != 2*time.Hour {
		t.Fatalf("renewal after = %s", result.RenewalAfter)
	}

	cached, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(renewed) {
		t.Fatal("renewed lease was not persisted")
	}
}

func TestClientRenewRejectsDifferentLicenseAndKeepsPreviousCache(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	installationID, err := store.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	initial := signer.signLease(t, installationID, "lic-renew-2", "nonce-old", nil)
	if err := store.SaveLease(initial); err != nil {
		t.Fatal(err)
	}

	transport := mockControlPlaneTransport{
		activate: func(context.Context, ActivationRequest) (LeaseResponse, error) {
			return LeaseResponse{}, errors.New("unexpected activate")
		},
		renew: func(_ context.Context, request RenewalRequest) (LeaseResponse, error) {
			replacement := signer.signLease(t, request.InstallationID, "different-license", "nonce-other", nil)
			return LeaseResponse{Lease: replacement, ServerTime: signer.now}, nil
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

	_, err = client.Renew(context.Background())
	if !errors.Is(err, ErrRenewalLicenseChanged) {
		t.Fatalf("error = %v, want %v", err, ErrRenewalLicenseChanged)
	}
	cached, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(initial) {
		t.Fatal("invalid renewal replaced the previous cached lease")
	}
}

func TestClientRenewTransportFailureKeepsPreviousCache(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	installationID, err := store.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	initial := signer.signLease(t, installationID, "lic-renew-3", "nonce-old", nil)
	if err := store.SaveLease(initial); err != nil {
		t.Fatal(err)
	}

	transport := mockControlPlaneTransport{
		activate: func(context.Context, ActivationRequest) (LeaseResponse, error) {
			return LeaseResponse{}, errors.New("unexpected activate")
		},
		renew: func(context.Context, RenewalRequest) (LeaseResponse, error) {
			return LeaseResponse{}, errors.New("temporary control plane failure")
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

	if _, err := client.Renew(context.Background()); err == nil {
		t.Fatal("renew transport failure unexpectedly succeeded")
	}
	cached, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(initial) {
		t.Fatal("transport failure changed cached lease")
	}
}
