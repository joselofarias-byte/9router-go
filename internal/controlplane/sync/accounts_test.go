package sync

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"9router/proxy/internal/controlplane/registry"
)

func TestSyncAccountsFromDB(t *testing.T) {
	tempDB := "test_sync_accounts.db"
	defer os.Remove(tempDB)

	db, _ := sql.Open("sqlite", tempDB)
	defer db.Close()
	_, _ = db.Exec(`
		CREATE TABLE providerConnections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			authType TEXT NOT NULL,
			isActive INTEGER DEFAULT 1,
			data TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);
	`)

	// Init registry to establish baseline in-memory state
	registry.InitRegistry(nil)

	now := time.Now().Format(time.RFC3339)
	_, _ = db.Exec(`INSERT INTO providerConnections (id, provider, authType, isActive, data, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"acc-1", "prov-1", "bearer", 1, "{}", now, now)

	_, _ = db.Exec(`INSERT INTO providerConnections (id, provider, authType, isActive, data, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"acc-2", "prov-2", "bearer", 0, "{}", now, now)

	SyncAccountsFromDB(db)

	state := registry.GetActiveState()
	if state == nil {
		t.Fatalf("expected state to be initialized")
	}

	if len(state.Accounts) != 2 {
		t.Fatalf("expected 2 synced accounts, got %d", len(state.Accounts))
	}

	if state.Accounts["acc-1"].ProviderID != "prov-1" || !state.Accounts["acc-1"].IsActive {
		t.Errorf("acc-1 incorrectly synced")
	}
	if state.Accounts["acc-2"].ProviderID != "prov-2" || state.Accounts["acc-2"].IsActive {
		t.Errorf("acc-2 incorrectly synced")
	}

	// Fast-path test: Calling again should hit generation check without re-parsing/syncing full table
	err := SyncAccountsFromDB(db)
	if err != nil {
		t.Errorf("fast-path sync failed: %v", err)
	}

	// Mutation test: Simulate update -> forces sync
	time.Sleep(1 * time.Second) // Ensure noticeable timestamp change
	newTime := time.Now().Format(time.RFC3339)
	_, _ = db.Exec(`UPDATE providerConnections SET isActive = 1, updatedAt = ? WHERE id = 'acc-2'`, newTime)

	err = SyncAccountsFromDB(db)
	if err != nil {
		t.Errorf("sync failed after mutation: %v", err)
	}

	state = registry.GetActiveState()
	if !state.Accounts["acc-2"].IsActive {
		t.Errorf("acc-2 state failed to update synchronously after mutation")
	}
}
