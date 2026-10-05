package entitlements

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeStoreRecoversInterruptedReplacement(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	if err := os.MkdirAll(store.Dir(), privateDirPerm); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"lease":"previous-complete"}`)
	backup := replacementBackup(store.leaseCachePath())
	if err := os.WriteFile(backup, want, privateFilePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), ".lease.json.tmp-stale"), []byte("partial"), privateFilePerm); err != nil {
		t.Fatal(err)
	}

	got, err := store.LoadLease()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("recovered lease = %q, want %q", got, want)
	}
	if _, err := os.Stat(store.leaseCachePath()); err != nil {
		t.Fatalf("recovered target missing: %v", err)
	}
}

func writeStoredInstallationID(t *testing.T, store *RuntimeStore, id string) {
	t.Helper()
	if err := atomicWriteFile(store.installationIDPath(), []byte(id+"\n")); err != nil {
		t.Fatal(err)
	}
}
