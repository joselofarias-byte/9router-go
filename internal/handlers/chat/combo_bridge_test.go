package chat

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"9router/proxy/internal/controlplane/discovery"
	"9router/proxy/internal/controlplane/registry"
)

func TestGetActiveCandidates(t *testing.T) {
	// Simple stub test to ensure bridge logic returns without panic
	registry.InitRegistry(nil)
	nodes := getActiveCandidates(context.Background(), "test-model")

	// Should be 0 since the registry state is empty/uninitialized here
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

// Stub Adapter to inject a candidate
type mockAdapter struct{}
func (m *mockAdapter) SourceID() string { return "mock" }
func (m *mockAdapter) Discover(ctx context.Context) ([]discovery.Candidate, error) {
	return []discovery.Candidate{
		{ProviderID: "test-prov", ModelID: "test-model", PricingMode: "paid"},
	}, nil
}

func TestActiveCandidatesWithDBSync(t *testing.T) {
	tempDB := "test_active_candidates.db"
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
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`)
	_, _ = db.Exec(`INSERT INTO providerConnections (id, provider, authType, isActive, data, createdAt, updatedAt)
		VALUES ('test-conn-1', 'test-prov', 'bearer', 1, '{}', ?, ?)`, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))

	// Init empty
	registry.InitRegistry(db)

	// Run orchestrator which should sync the DB accounts + adapter candidate -> new active snapshot
	orch := discovery.NewOrchestrator(db, []discovery.Adapter{&mockAdapter{}})
	orch.RunSync(context.Background())

	// Now Data Plane should correctly find the route
	nodes := getActiveCandidates(context.Background(), "test-model")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node route, got %d", len(nodes))
	}

	if nodes[0].ProviderID != "test-prov" || nodes[0].AccountID != "test-conn-1" {
		t.Errorf("expected test-prov with test-conn-1, got %s / %s", nodes[0].ProviderID, nodes[0].AccountID)
	}
}
