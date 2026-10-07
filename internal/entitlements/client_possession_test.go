package entitlements

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestClientCopiedKeyBackedInstallationWithoutProofKeyFailsClosed(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	raw := cacheLeaseForInstall(t, store, signer, identity.ID, true)
	if err := os.Remove(store.installationKeyPath()); err != nil {
		t.Fatal(err)
	}

	var calls int
	client := testLicenseClient(t, store, signer, rejectingTransport(&calls))
	if _, err := client.Renew(context.Background()); !errors.Is(err, ErrInvalidInstallationKey) {
		t.Fatalf("renew error = %v, want %v", err, ErrInvalidInstallationKey)
	}
	if err := client.Release(context.Background()); !errors.Is(err, ErrInvalidInstallationKey) {
		t.Fatalf("release error = %v, want %v", err, ErrInvalidInstallationKey)
	}
	if calls != 0 {
		t.Fatalf("control plane calls = %d", calls)
	}
	assertCachedLease(t, store, raw)
	if _, err := os.Stat(store.installationKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("renew or release minted a replacement proof key")
	}
	if _, err := client.Activate(context.Background(), "other-code"); !errors.Is(err, ErrInstallationKeyMismatch) {
		t.Fatalf("activate error = %v, want %v", err, ErrInstallationKeyMismatch)
	}
	if calls != 0 {
		t.Fatalf("control plane calls = %d", calls)
	}
	assertCachedLease(t, store, raw)
}

func TestClientCopiedLegacyInstallationWithoutProofKeyCannotRenewOrRelease(t *testing.T) {
	const legacyID = "11111111-2222-4333-8444-555555555555"
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	writeStoredInstallationID(t, store, legacyID)
	raw := cacheLeaseForInstall(t, store, signer, legacyID, true)

	var calls int
	client := testLicenseClient(t, store, signer, rejectingTransport(&calls))
	if _, err := client.Renew(context.Background()); !errors.Is(err, ErrInvalidInstallationKey) {
		t.Fatalf("renew error = %v, want %v", err, ErrInvalidInstallationKey)
	}
	if err := client.Release(context.Background()); !errors.Is(err, ErrInvalidInstallationKey) {
		t.Fatalf("release error = %v, want %v", err, ErrInvalidInstallationKey)
	}
	if calls != 0 {
		t.Fatalf("control plane calls = %d", calls)
	}
	if _, err := os.Stat(store.installationKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing proof key stat = %v", err)
	}
	assertCachedLease(t, store, raw)
}

func TestClientLegacyInstallationWithProofKeyCanRenew(t *testing.T) {
	const legacyID = "22222222-3333-4444-8555-666666666666"
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	writeStoredInstallationID(t, store, legacyID)
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != legacyID {
		t.Fatalf("installation id = %q", identity.ID)
	}
	cacheLeaseForInstall(t, store, signer, legacyID, true)

	var calls int
	transport := mockControlPlaneTransport{
		renew: func(_ context.Context, request RenewalRequest) (LeaseResponse, error) {
			calls++
			if request.InstallationID != legacyID || request.LicenseID != "lic-cached" || request.CurrentNonce != "nonce-cached" {
				t.Fatalf("renewal request = %+v", request)
			}
			if err := VerifyRenewalProof(request, identity.PublicKey); err != nil {
				t.Fatalf("legacy renewal proof rejected: %v", err)
			}
			renewed := signer.signLease(t, legacyID, "lic-cached", "nonce-next", nil)
			return LeaseResponse{Lease: renewed, ServerTime: signer.now}, nil
		},
		release: func(context.Context, ReleaseRequest) error {
			t.Fatal("unexpected release")
			return nil
		},
	}
	client := testLicenseClient(t, store, signer, transport)
	result, err := client.Renew(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("renew calls = %d", calls)
	}
	if result.Evaluation.Lease.Nonce != "nonce-next" {
		t.Fatalf("nonce = %q", result.Evaluation.Lease.Nonce)
	}
}

func TestClientCachedLeaseWithoutTrustedTimeCannotRenewOrRelease(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	raw := cacheLeaseForInstall(t, store, signer, identity.ID, false)

	var calls int
	client := testLicenseClient(t, store, signer, rejectingTransport(&calls))
	if _, err := client.Renew(context.Background()); !errors.Is(err, ErrTrustedTimeRequired) {
		t.Fatalf("renew error = %v, want %v", err, ErrTrustedTimeRequired)
	}
	if err := client.Release(context.Background()); !errors.Is(err, ErrTrustedTimeRequired) {
		t.Fatalf("release error = %v, want %v", err, ErrTrustedTimeRequired)
	}
	if calls != 0 {
		t.Fatalf("control plane calls = %d", calls)
	}
	assertCachedLease(t, store, raw)
}

func TestClientCorruptTrustedTimeCannotRenewOrRelease(t *testing.T) {
	signer := newClientTestSigner(t)
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	raw := cacheLeaseForInstall(t, store, signer, identity.ID, false)
	if err := os.WriteFile(store.lastTrustedTimePath(), []byte("not-a-time\n"), privateFilePerm); err != nil {
		t.Fatal(err)
	}

	var calls int
	client := testLicenseClient(t, store, signer, rejectingTransport(&calls))
	if _, err := client.Renew(context.Background()); err == nil {
		t.Fatal("renew accepted corrupt trusted time")
	}
	if err := client.Release(context.Background()); err == nil {
		t.Fatal("release accepted corrupt trusted time")
	}
	if calls != 0 {
		t.Fatalf("control plane calls = %d", calls)
	}
	assertCachedLease(t, store, raw)
}

func cacheLeaseForInstall(t *testing.T, store *RuntimeStore, signer clientTestSigner, installationID string, trust bool) []byte {
	t.Helper()
	raw := signer.signLease(t, installationID, "lic-cached", "nonce-cached", nil)
	if trust {
		if _, err := store.AdvanceTrustedTime(signer.now); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func testLicenseClient(t *testing.T, store *RuntimeStore, signer clientTestSigner, transport ControlPlaneTransport) *Client {
	t.Helper()
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
	return client
}

func rejectingTransport(calls *int) mockControlPlaneTransport {
	bump := func() {
		*calls++
	}
	return mockControlPlaneTransport{
		activate: func(context.Context, ActivationRequest) (LeaseResponse, error) {
			bump()
			return LeaseResponse{}, errors.New("unexpected activate")
		},
		renew: func(context.Context, RenewalRequest) (LeaseResponse, error) {
			bump()
			return LeaseResponse{}, errors.New("unexpected renew")
		},
		release: func(context.Context, ReleaseRequest) error {
			bump()
			return errors.New("unexpected release")
		},
	}
}

func assertCachedLease(t *testing.T, store *RuntimeStore, want []byte) {
	t.Helper()
	cached, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(want) {
		t.Fatal("cached lease changed")
	}
}
