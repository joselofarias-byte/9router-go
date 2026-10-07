package entitlements

import (
	"errors"
	"testing"
	"time"
)

func TestRuntimeStoreBuildHardExpiryFallsBackToCommunity(t *testing.T) {
	const installationID = "51111111-2222-4333-8444-555555555555"
	raw, keys, ctx, _ := signedLeaseFixture(t, func(lease *Lease) {
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

	build := ctx.Build
	build.ProCapableUntil = ctx.Now
	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  keys,
		Build: build,
		Now:   func() time.Time { return ctx.Now },
	})
	if !errors.Is(err, ErrBuildExpired) {
		t.Fatalf("error = %v, want %v", err, ErrBuildExpired)
	}
	if state.Provider.Status().Mode != "community" {
		t.Fatalf("mode = %q, want community", state.Provider.Status().Mode)
	}
}

func TestRuntimeStoreClockRollbackFallsBackToCommunity(t *testing.T) {
	const installationID = "61111111-2222-4333-8444-555555555555"
	raw, keys, ctx, _ := signedLeaseFixture(t, func(lease *Lease) {
		lease.InstallationID = installationID
	})
	store := NewRuntimeStore(t.TempDir())
	writeStoredInstallationID(t, store, installationID)
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdvanceTrustedTime(ctx.Now.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}

	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  keys,
		Build: ctx.Build,
		Now:   func() time.Time { return ctx.Now },
	})
	if !errors.Is(err, ErrClockRollback) {
		t.Fatalf("error = %v, want %v", err, ErrClockRollback)
	}
	if state.Provider.Status().Mode != "community" {
		t.Fatalf("mode = %q, want community", state.Provider.Status().Mode)
	}
}
