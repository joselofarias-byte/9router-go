package entitlements

import (
	"testing"
	"time"
)

func TestRuntimeStoreFirstRunIsCommunitySafe(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	store := NewRuntimeStore(t.TempDir())
	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  KeyRing{},
		Build: BuildIdentity{Channel: "beta"},
		Now:   func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Provider.Status().Mode != "community" {
		t.Fatalf("mode = %q, want community", state.Provider.Status().Mode)
	}
}

func TestRuntimeStoreLoadsActiveAndGraceLease(t *testing.T) {
	const installationID = "11111111-2222-4333-8444-555555555555"
	raw, keys, ctx, lease := signedLeaseFixture(t, func(lease *Lease) {
		lease.InstallationID = installationID
	})

	store := NewRuntimeStore(t.TempDir())
	writeStoredInstallationID(t, store, installationID)
	if err := store.SaveLease(raw); err != nil {
		t.Fatal(err)
	}

	now := ctx.Now
	state, err := store.LoadRuntime(RuntimeOptions{
		Keys:  keys,
		Build: ctx.Build,
		Now:   func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	status := state.Provider.Status()
	if status.Mode != "licensed" || status.State != string(LeaseStateActive) {
		t.Fatalf("active status = %+v", status)
	}

	now = lease.ExpiresAt.Add(time.Hour)
	state, err = store.LoadRuntime(RuntimeOptions{
		Keys:  keys,
		Build: ctx.Build,
		Now:   func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	status = state.Provider.Status()
	if status.Mode != "licensed" || status.State != string(LeaseStateGrace) {
		t.Fatalf("grace status = %+v", status)
	}
}
