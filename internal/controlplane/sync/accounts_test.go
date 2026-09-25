package sync

import (
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"
	_ "modernc.org/sqlite"
)

func resetSyncState() {
	genMu.Lock()
	lastSyncGen = 0
	hasSyncedOnce = false
	genMu.Unlock()
}

func TestSyncAccountsFromDB(t *testing.T) {
	resetSyncState()
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
	_, _ = db.Exec(`CREATE TABLE controlplane_meta (key TEXT PRIMARY KEY, val INTEGER NOT NULL);`)
	_, _ = db.Exec(`INSERT INTO controlplane_meta (key, val) VALUES ('accounts_generation', 1);`)
	_, _ = db.Exec(`
		CREATE TRIGGER trg_providerConnections_after_insert AFTER INSERT ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
	`)
	_, _ = db.Exec(`
		CREATE TRIGGER trg_providerConnections_after_update AFTER UPDATE ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
	`)
	_, _ = db.Exec(`
		CREATE TRIGGER trg_providerConnections_after_delete AFTER DELETE ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
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

	// Same-second Mutation test: Simulate update -> forces sync even without timestamp change
	sameTime := now
	_, _ = db.Exec(`UPDATE providerConnections SET isActive = 1, updatedAt = ? WHERE id = 'acc-2'`, sameTime)

	err = SyncAccountsFromDB(db)
	if err != nil {
		t.Errorf("sync failed after same-second mutation: %v", err)
	}

	state = registry.GetActiveState()
	if !state.Accounts["acc-2"].IsActive {
		t.Errorf("acc-2 state failed to update synchronously after mutation")
	}
}

func TestSyncAccountsFromDB_PartialFailureRetries(t *testing.T) {
	tempDB := "test_sync_accounts_partial.db"
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
	_, _ = db.Exec(`CREATE TABLE controlplane_meta (key TEXT PRIMARY KEY, val INTEGER NOT NULL);`)
	_, _ = db.Exec(`INSERT INTO controlplane_meta (key, val) VALUES ('accounts_generation', 1);`)
	_, _ = db.Exec(`
		CREATE TRIGGER trg_providerConnections_after_insert AFTER INSERT ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
	`)
	_, _ = db.Exec(`
		CREATE TRIGGER trg_providerConnections_after_update AFTER UPDATE ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
	`)
	_, _ = db.Exec(`
		CREATE TRIGGER trg_providerConnections_after_delete AFTER DELETE ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
	`)

	registry.InitRegistry(nil)

	now := time.Now().Format(time.RFC3339)
	_, _ = db.Exec(`INSERT INTO providerConnections (id, provider, authType, isActive, data, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"acc-1", "prov-1", "bearer", 1, "{}", now, now)

	// Ensure clean initial state
	resetSyncState()

	// Force the full scan query to fail by injecting a bad query
	originalQuery := queryFullAccounts
	queryFullAccounts = "SELECT syntax_error FROM missing_table"

	err := SyncAccountsFromDB(db)
	if err == nil {
		t.Fatalf("expected error from forced query failure")
	}

	// Fast-path should NOT be primed because the full scan failed
	genMu.RLock()
	if lastSyncGen != 0 || hasSyncedOnce {
		t.Errorf("lastSyncGen or hasSyncedOnce was improperly advanced despite failure")
	}
	genMu.RUnlock()

	// Restore query. The next sync should succeed and perform a full scan.
	queryFullAccounts = originalQuery

	err = SyncAccountsFromDB(db)
	if err != nil {
		t.Fatalf("expected next sync to succeed, got %v", err)
	}

	state := registry.GetActiveState()
	if len(state.Accounts) != 1 {
		t.Errorf("expected account to finally be synced upon retry")
	}
}

func TestSyncAccountsFromDB_EmptyDB(t *testing.T) {
	tempDB := "test_sync_accounts_empty.db"
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
	_, _ = db.Exec(`CREATE TABLE controlplane_meta (key TEXT PRIMARY KEY, val INTEGER NOT NULL);`)
	_, _ = db.Exec(`INSERT INTO controlplane_meta (key, val) VALUES ('accounts_generation', 1);`)

	// Pre-seed the registry with a stale account to verify it gets cleared
	registry.InitRegistry(nil)
	registry.UpdateAccounts(map[string]*registry.Account{
		"stale-acc": {ID: "stale-acc", ProviderID: "stale-prov", IsActive: true},
	})

	resetSyncState()

	err := SyncAccountsFromDB(db)
	if err != nil {
		t.Fatalf("sync failed on empty db: %v", err)
	}

	state := registry.GetActiveState()
	if len(state.Accounts) != 0 {
		t.Errorf("expected 0 accounts after syncing from empty DB, got %d", len(state.Accounts))
	}
}

func TestPublishAccounts_IgnoresStaleGeneration(t *testing.T) {
	registry.InitRegistry(nil)
	resetSyncState()
	genMu.Lock()
	lastSyncGen = 6
	hasSyncedOnce = true
	genMu.Unlock()
	registry.UpdateAccounts(map[string]*registry.Account{
		"new": {ID: "new", ProviderID: "p", IsActive: true},
	})

	if publishAccounts(5, true, map[string]*registry.Account{
		"old": {ID: "old", ProviderID: "p", IsActive: true},
	}) {
		t.Fatal("stale generation was published")
	}
	state := registry.GetActiveState()
	if _, exists := state.Accounts["old"]; exists {
		t.Fatal("stale account overwrote the newer snapshot")
	}
	if _, exists := state.Accounts["new"]; !exists {
		t.Fatal("newer account missing after stale publish")
	}

	if !publishAccounts(7, true, map[string]*registry.Account{
		"newer": {ID: "newer", ProviderID: "p", IsActive: false},
	}) {
		t.Fatal("newer generation was rejected")
	}
	state = registry.GetActiveState()
	if state.Accounts["newer"] == nil || state.Accounts["newer"].IsActive {
		t.Fatalf("newer generation did not replace accounts: %+v", state.Accounts)
	}
}

func TestRefreshAccountsAfterSnapshot_DropsResurrectedConnection(t *testing.T) {
	tempDB := "test_sync_accounts_refresh.db"
	defer os.Remove(tempDB)

	db, err := sql.Open("sqlite", tempDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE providerConnections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			authType TEXT NOT NULL,
			isActive INTEGER DEFAULT 1,
			data TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);
		CREATE TABLE controlplane_meta (key TEXT PRIMARY KEY, val INTEGER NOT NULL);
		INSERT INTO controlplane_meta (key, val) VALUES ('accounts_generation', 1);
		CREATE TRIGGER trg_providerConnections_after_insert AFTER INSERT ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
		CREATE TRIGGER trg_providerConnections_after_update AFTER UPDATE ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
		CREATE TRIGGER trg_providerConnections_after_delete AFTER DELETE ON providerConnections BEGIN UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation'; END;
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}

	registry.InitRegistry(nil)
	resetSyncState()

	now := time.Now().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO providerConnections (id, provider, authType, isActive, data, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"acc-1", "groq", "bearer", 1, "{}", now, now); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := SyncAccountsFromDB(db); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if _, err := db.Exec(`UPDATE providerConnections SET isActive = 0 WHERE id = 'acc-1'`); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if err := SyncAccountsFromDB(db); err != nil {
		t.Fatalf("sync after deactivate: %v", err)
	}
	if registry.GetActiveState().Accounts["acc-1"].IsActive {
		t.Fatal("deactivated connection still active after sync")
	}

	// A discovery snapshot can publish an older account view and leave the
	// generation fast path believing it is current.
	registry.UpdateAccounts(map[string]*registry.Account{
		"acc-1": {ID: "acc-1", ProviderID: "groq", IsActive: true},
	})
	if err := RefreshAccountsAfterSnapshot(db); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	acc := registry.GetActiveState().Accounts["acc-1"]
	if acc == nil || acc.IsActive {
		t.Fatalf("resurrected connection was not cleared: %+v", acc)
	}
}

func TestSyncAccountsFromDB_Concurrency(t *testing.T) {
	tempDB := "test_sync_accounts_conc.db"
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
	_, _ = db.Exec(`CREATE TABLE controlplane_meta (key TEXT PRIMARY KEY, val INTEGER NOT NULL);`)
	_, _ = db.Exec(`INSERT INTO controlplane_meta (key, val) VALUES ('accounts_generation', 1);`)

	registry.InitRegistry(nil)
	resetSyncState()

	// Launch multiple goroutines calling SyncAccountsFromDB concurrently
	// Run this test with `-race`
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = SyncAccountsFromDB(db)
		}(i)
	}
	wg.Wait()
}
