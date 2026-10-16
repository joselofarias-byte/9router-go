package discovery

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"
	cpsync "9router/proxy/internal/controlplane/sync"

	_ "modernc.org/sqlite"
)

type blockingDiscoveryAdapter struct {
	started chan struct{}
	release chan struct{}
}

func (a *blockingDiscoveryAdapter) SourceID() string { return "blocking-test" }

func (a *blockingDiscoveryAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	close(a.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.release:
	}
	return []Candidate{{
		SourceID:      a.SourceID(),
		ProviderID:    "prov",
		ModelID:       "model",
		UpstreamModel: "model",
		PricingMode:   "free_tier",
	}}, nil
}

func TestOrchestratorDoesNotOverwriteNewerAccountSync(t *testing.T) {
	f, err := os.CreateTemp("", "9router-orchestrator-race-*.db")
	if err != nil {
		t.Fatalf("temp db: %v", err)
	}
	dbPath := f.Name()
	f.Close()
	defer os.Remove(dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE providerConnections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			authType TEXT NOT NULL,
			name TEXT,
			email TEXT,
			priority INTEGER,
			isActive INTEGER DEFAULT 1,
			data TEXT NOT NULL,
			lastUsedAt TEXT,
			consecutiveUseCount INTEGER DEFAULT 0,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);
		CREATE TABLE controlplane_meta (
			key TEXT PRIMARY KEY,
			val INTEGER NOT NULL
		);
		INSERT INTO controlplane_meta (key, val) VALUES ('accounts_generation', 1);
		CREATE TRIGGER trg_providerConnections_after_insert
		AFTER INSERT ON providerConnections
		BEGIN
			UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation';
		END;
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
		t.Fatalf("create schema: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	insertAccount := func(id string) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO providerConnections
				(id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
			VALUES (?, 'prov', 'apikey', ?, 1, 1, '{}', ?, ?)
		`, id, id, now, now); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	registry.InitRegistry(nil)
	insertAccount("acc-a")
	if err := cpsync.SyncAccountsFromDB(db); err != nil {
		t.Fatalf("initial account sync: %v", err)
	}
	if registry.GetActiveState().Accounts["acc-a"] == nil {
		t.Fatal("initial account acc-a was not synced")
	}

	adapter := &blockingDiscoveryAdapter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	orchestrator := NewOrchestrator(db, []Adapter{adapter})
	done := make(chan struct{})
	go func() {
		orchestrator.RunSync(context.Background())
		close(done)
	}()

	select {
	case <-adapter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not reach blocking adapter")
	}

	// Discovery has already copied the old account set. Publish a newer DB
	// generation and sync it into the active registry while discovery is paused.
	insertAccount("acc-b")
	if err := cpsync.SyncAccountsFromDB(db); err != nil {
		t.Fatalf("sync newer account generation: %v", err)
	}
	if registry.GetActiveState().Accounts["acc-b"] == nil {
		t.Fatal("acc-b should be visible before discovery resumes")
	}

	close(adapter.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not finish")
	}

	// Contract: publishing a discovery snapshot must not roll Accounts back to
	// the stale copy captured before acc-b existed.
	if registry.GetActiveState().Accounts["acc-b"] == nil {
		t.Fatal("discovery activation overwrote newer account state: acc-b disappeared")
	}

	// A generation-based fast path must also remain able to repair state.
	if err := cpsync.SyncAccountsFromDB(db); err != nil {
		t.Fatalf("post-discovery account sync: %v", err)
	}
	if registry.GetActiveState().Accounts["acc-b"] == nil {
		t.Fatal("generation fast path left the registry on stale account state")
	}
}
