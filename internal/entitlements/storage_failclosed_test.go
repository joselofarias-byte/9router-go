package entitlements

import (
	"errors"
	"testing"
	"time"
)

func TestRuntimeStoreInvalidLeaseFallsBackToCommunity(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	if _, err := store.InstallationID(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLease([]byte("{broken")); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  KeyRing{},
		Build: BuildIdentity{Channel: "beta"},
		Now:   func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC) },
	})
	if err == nil {
		t.Fatal("invalid lease unexpectedly verified")
	}
	if state.Provider.Status().Mode != "community" {
		t.Fatalf("mode = %q, want community", state.Provider.Status().Mode)
	}
}

func TestRuntimeStoreFinalExpiryFallsBackToCommunity(t *testing.T) {
	const installationID = "21111111-2222-4333-8444-555555555555"
	raw, keys, ctx, lease := signedLeaseFixture(t, func(lease *Lease) {
		lease.InstallationID = installationID
	})
	store := NewRuntimeStore(t.TempDir())
	writeStoredInstallationID(t, store, installationID)
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdvanceTrustedTime(ctx.Now); err != nil {
		t.Fatal(err)
	}

	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  keys,
		Build: ctx.Build,
		Now:   func() time.Time { return lease.GraceUntil },
	})
	if !errors.Is(err, ErrExpiredLease) {
		t.Fatalf("error = %v, want %v", err, ErrExpiredLease)
	}
	if state.Provider.Status().Mode != "community" {
		t.Fatalf("mode = %q, want community", state.Provider.Status().Mode)
	}
}

func TestRuntimeStoreWrongInstallationFallsBackToCommunity(t *testing.T) {
	const leaseInstallation = "31111111-2222-4333-8444-555555555555"
	const localInstallation = "41111111-2222-4333-8444-555555555555"
	raw, keys, ctx, _ := signedLeaseFixture(t, func(lease *Lease) {
		lease.InstallationID = leaseInstallation
	})
	store := NewRuntimeStore(t.TempDir())
	writeStoredInstallationID(t, store, localInstallation)
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdvanceTrustedTime(ctx.Now); err != nil {
		t.Fatal(err)
	}

	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  keys,
		Build: ctx.Build,
		Now:   func() time.Time { return ctx.Now },
	})
	if !errors.Is(err, ErrWrongInstallation) {
		t.Fatalf("error = %v, want %v", err, ErrWrongInstallation)
	}
	if state.Provider.Status().Mode != "community" {
		t.Fatalf("mode = %q, want community", state.Provider.Status().Mode)
	}
}
