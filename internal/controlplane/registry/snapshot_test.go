package registry

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSnapshotCycle(t *testing.T) {
	tempDB := "test_snapshots.db"
	defer os.Remove(tempDB)

	db, err := sql.Open("sqlite", tempDB)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Create table manually for this test since we are bypassing the migration runner
	_, err = db.Exec(`
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	state := &RegistryState{
		Providers: make(map[string]*Provider),
		Models:    make(map[string]*Model),
		ProviderModels: make(map[string]map[string]*ProviderModel),
		Accounts:  make(map[string]*Account),
	}

	snap, err := CreateSnapshot(db, state, "initialization")
	if err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}

	if snap.Status != "candidate" {
		t.Errorf("expected status 'candidate', got %s", snap.Status)
	}

	if err := ActivateSnapshot(db, snap.Version); err != nil {
		t.Fatalf("failed to activate snapshot: %v", err)
	}

	active, err := GetActiveSnapshot(db)
	if err != nil {
		t.Fatalf("failed to get active snapshot: %v", err)
	}

	if active.Version != snap.Version {
		t.Errorf("expected version %s, got %s", snap.Version, active.Version)
	}
}
