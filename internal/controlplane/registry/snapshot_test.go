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

func TestSnapshotChecksumValidation(t *testing.T) {
	tempDB := "test_snapshots_validation.db"
	defer os.Remove(tempDB)

	db, _ := sql.Open("sqlite", tempDB)
	defer db.Close()
	_, _ = db.Exec(`
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`)

	// Insert tampered snapshot
	payload := `{"Providers":{}}`
	_, _ = db.Exec(
		`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		"bad-version", "2026-01-01T00:00:00Z", "test", "tampered-checksum-123", "active", payload,
	)

	// Validate Checksum fails
	err := ActivateSnapshot(db, "bad-version")
	if err == nil {
		t.Fatalf("expected error on tampered checksum activate, got nil")
	}

	// Insert a valid LKG snapshot
	validPayload := `{"Providers":{"test":{"id":"test"}}}`
	validChecksum := GenerateChecksum(validPayload)
	_, _ = db.Exec(
		`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		"lkg-version", "2026-01-01T00:00:01Z", "test-lkg", validChecksum, "last_known_good", validPayload,
	)

	// InitRegistry should gracefully fail active checksum parsing, fallback to LKG, and succeed.
	err = InitRegistry(db)
	if err != nil {
		t.Fatalf("expected InitRegistry to recover from LKG gracefully without surfacing error, got %v", err)
	}

	state := GetActiveState()
	if state == nil {
		t.Fatalf("expected InitRegistry to load LKG state fallback, got nil state")
	}
	if state.Providers["test"] == nil {
		t.Fatalf("expected LKG state to have test provider, but did not")
	}
}

func TestSnapshotLKG_CorruptChain(t *testing.T) {
	tempDB := "test_snapshots_corrupt_chain.db"
	defer os.Remove(tempDB)

	db, _ := sql.Open("sqlite", tempDB)
	defer db.Close()
	_, _ = db.Exec(`
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`)

	// 1. Insert a very old valid LKG
	validOldPayload := `{"Providers":{"old":{"id":"old"}}}`
	_, _ = db.Exec(
		`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		"lkg-old", "2026-01-01T00:00:01Z", "test-lkg", GenerateChecksum(validOldPayload), "last_known_good", validOldPayload,
	)

	// 2. Insert a newer but CORRUPT LKG (simulating what would happen if a bad active was blindly moved to LKG)
	corruptPayload := `{"Providers":{"corrupt":{"id":"corrupt"}}}`
	_, _ = db.Exec(
		`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		"lkg-new-corrupt", "2026-01-02T00:00:01Z", "test-lkg", "bad-checksum", "last_known_good", corruptPayload,
	)

	// 3. Insert an active snapshot that is also corrupt
	_, _ = db.Exec(
		`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		"active-corrupt", "2026-01-03T00:00:00Z", "test", "bad-checksum-2", "active", corruptPayload,
	)

	// InitRegistry should skip the corrupt active, skip the corrupt new LKG, and recover from the old valid LKG.
	err := InitRegistry(db)
	if err != nil {
		t.Fatalf("expected InitRegistry to recover seamlessly, got %v", err)
	}

	state := GetActiveState()
	if state == nil || state.Providers["old"] == nil {
		t.Fatalf("expected to recover from oldest valid LKG state, but failed")
	}

	// Verify the corrupt active snapshot was flagged as corrupt
	var activeStatus string
	_ = db.QueryRow("SELECT status FROM registry_snapshots WHERE version = 'active-corrupt'").Scan(&activeStatus)
	if activeStatus != "corrupt" {
		t.Errorf("expected active snapshot to be marked corrupt, got %s", activeStatus)
	}
}
