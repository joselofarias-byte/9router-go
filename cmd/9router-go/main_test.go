package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	_ "modernc.org/sqlite"
)

func TestInitializeControlPlaneForServingRefreshesSnapshotAccounts(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "startup.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
		CREATE TABLE providerConnections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			isActive INTEGER NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);
		CREATE TABLE controlplane_meta (
			key TEXT PRIMARY KEY,
			val INTEGER NOT NULL
		);
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	stale := &registry.RegistryState{
		Providers:      map[string]*registry.Provider{},
		Models:         map[string]*registry.Model{},
		ProviderModels: map[string]map[string]*registry.ProviderModel{},
		Accounts: map[string]*registry.Account{
			"stale-account": {ID: "stale-account", ProviderID: "deepseek", IsActive: true},
		},
	}
	payload, err := stale.ToJSON()
	if err != nil {
		t.Fatalf("serialize stale snapshot: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO registry_snapshots
		(version, created_at, reason, checksum, status, payload)
		VALUES ('stale-v1', '2026-10-07T10:00:00Z', 'test', ?, 'active', ?)
	`, registry.GenerateChecksum(payload), payload); err != nil {
		t.Fatalf("insert stale snapshot: %v", err)
	}

	if _, err := database.Exec(`
		INSERT INTO providerConnections
		(id, provider, isActive, createdAt, updatedAt)
		VALUES ('fresh-account', 'deepseek', 1, '2026-10-07T10:01:00Z', '2026-10-07T10:01:00Z')
	`); err != nil {
		t.Fatalf("insert fresh account: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO controlplane_meta (key, val)
		VALUES ('accounts_generation', 91007001)
	`); err != nil {
		t.Fatalf("insert generation: %v", err)
	}

	if err := initializeControlPlaneForServing(database); err != nil {
		t.Fatalf("initialize control plane: %v", err)
	}

	state := registry.GetActiveState()
	if state == nil {
		t.Fatal("expected active registry state")
	}
	if state.Accounts["fresh-account"] == nil {
		t.Fatalf("fresh DB account missing after startup sync: %#v", state.Accounts)
	}
	if state.Accounts["stale-account"] != nil {
		t.Fatalf("stale snapshot account survived startup sync: %#v", state.Accounts)
	}
}


func TestInitializeControlPlaneForServingFailsWithoutAuthoritativeAccounts(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "startup-missing-accounts.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("create registry table: %v", err)
	}

	if err := initializeControlPlaneForServing(database); err == nil {
		t.Fatal("expected startup to fail when providerConnections is unavailable")
	}
}
