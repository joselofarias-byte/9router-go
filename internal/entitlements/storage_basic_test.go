package entitlements

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRuntimeStoreInstallationIDStableAndDistinct(t *testing.T) {
	storeA := NewRuntimeStore(t.TempDir())
	first, err := storeA.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := storeA.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("installation id changed: %q != %q", first, second)
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("installation id is not UUID: %v", err)
	}

	storeB := NewRuntimeStore(t.TempDir())
	other, err := storeB.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("different data directories received the same installation id")
	}
}

func TestRuntimeStoreCorruptInstallationIDFailsClosed(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	if err := os.MkdirAll(store.Dir(), privateDirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.installationIDPath(), []byte("not-a-uuid\n"), privateFilePerm); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InstallationID(); err == nil {
		t.Fatal("corrupt installation id unexpectedly regenerated")
	}
}

func TestRuntimeStoreLeaseRoundTrip(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	want := []byte(`{"signed":"lease"}`)
	if err := store.SaveLease(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRuntimeStoreTrustedTimeOnlyMovesForward(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	earlier := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)

	if _, err := store.AdvanceTrustedTime(later); err != nil {
		t.Fatal(err)
	}
	got, err := store.AdvanceTrustedTime(earlier)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(later) {
		t.Fatalf("trusted time moved backward to %s", got)
	}
}
