package discovery

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"

	_ "modernc.org/sqlite"
)

type orchestratorTestAdapter struct {
	source     string
	candidates []Candidate
	err        error
}

func (a orchestratorTestAdapter) SourceID() string { return a.source }

func (a orchestratorTestAdapter) Discover(context.Context) ([]Candidate, error) {
	return a.candidates, a.err
}

func setupOrchestratorTestDB(t *testing.T, initial *registry.RegistryState) (*sql.DB, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "9router-orchestrator-*.db")
	if err != nil {
		t.Fatalf("temp db: %v", err)
	}
	name := f.Name()
	f.Close()

	db, err := sql.Open("sqlite", name)
	if err != nil {
		os.Remove(name)
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`); err != nil {
		db.Close()
		os.Remove(name)
		t.Fatalf("create registry_snapshots: %v", err)
	}

	snap, err := registry.CreateSnapshot(db, initial, "test-initial")
	if err != nil {
		db.Close()
		os.Remove(name)
		t.Fatalf("create initial snapshot: %v", err)
	}
	if err := registry.ActivateSnapshot(db, snap.Version); err != nil {
		db.Close()
		os.Remove(name)
		t.Fatalf("activate initial snapshot: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(name)
	}
	return db, cleanup
}

func orchestratorInitialState() *registry.RegistryState {
	now := time.Now().UTC()
	return &registry.RegistryState{
		Providers: map[string]*registry.Provider{
			"p": {ID: "p", Name: "p", IsActive: true, CreatedAt: now, UpdatedAt: now},
		},
		Models: map[string]*registry.Model{
			"old": {ID: "old", Name: "old", CreatedAt: now, UpdatedAt: now},
		},
		ProviderModels: map[string]map[string]*registry.ProviderModel{
			"p": {
				"old": {
					ProviderID: "p", ModelID: "old", UpstreamModel: "old-upstream",
					PricingMode: "free_tier", CostMetadata: "old-cost",
					Capabilities: "old-caps", IsActive: true, CreatedAt: now, UpdatedAt: now,
				},
			},
		},
		Accounts: map[string]*registry.Account{},
	}
}

func TestOrchestratorRunSyncRefreshesExistingOfferMetadata(t *testing.T) {
	initial := orchestratorInitialState()
	db, cleanup := setupOrchestratorTestDB(t, initial)
	defer cleanup()

	adapter := orchestratorTestAdapter{
		source: "test",
		candidates: []Candidate{{
			SourceID: "test", ProviderID: "p", ModelID: "old",
			UpstreamModel: "new-upstream", PricingMode: "paid",
			CostMetadata: "new-cost", Capabilities: "new-caps",
		}},
	}

	NewOrchestrator(db, []Adapter{adapter}).RunSync(context.Background())

	pm := registry.GetActiveState().ProviderModels["p"]["old"]
	if pm == nil {
		t.Fatal("expected provider model")
	}
	if pm.UpstreamModel != "new-upstream" || pm.PricingMode != "paid" || pm.CostMetadata != "new-cost" || pm.Capabilities != "new-caps" {
		t.Fatalf("stale offer metadata after sync: %+v", pm)
	}
	if !pm.IsActive {
		t.Fatal("rediscovered offer should be active")
	}
}

func TestOrchestratorRunSyncDeactivatesMissingOfferAfterCompletePass(t *testing.T) {
	initial := orchestratorInitialState()
	db, cleanup := setupOrchestratorTestDB(t, initial)
	defer cleanup()

	adapter := orchestratorTestAdapter{
		source: "test",
		candidates: []Candidate{{
			SourceID: "test", ProviderID: "p", ModelID: "replacement",
			UpstreamModel: "replacement", PricingMode: "free_tier",
		}},
	}

	NewOrchestrator(db, []Adapter{adapter}).RunSync(context.Background())

	state := registry.GetActiveState()
	if state.ProviderModels["p"]["old"].IsActive {
		t.Fatal("offer omitted from complete provider catalog remained active")
	}
	if replacement := state.ProviderModels["p"]["replacement"]; replacement == nil || !replacement.IsActive {
		t.Fatal("replacement offer should be active")
	}
}

func TestOrchestratorRunSyncPreservesMissingOfferWhenAnyAdapterFails(t *testing.T) {
	initial := orchestratorInitialState()
	db, cleanup := setupOrchestratorTestDB(t, initial)
	defer cleanup()

	success := orchestratorTestAdapter{
		source: "success",
		candidates: []Candidate{{
			SourceID: "success", ProviderID: "p", ModelID: "replacement",
			UpstreamModel: "replacement", PricingMode: "free_tier",
		}},
	}
	failed := orchestratorTestAdapter{source: "failed", err: errors.New("catalog unavailable")}

	NewOrchestrator(db, []Adapter{success, failed}).RunSync(context.Background())

	state := registry.GetActiveState()
	if !state.ProviderModels["p"]["old"].IsActive {
		t.Fatal("transient adapter failure must preserve prior offer liveness")
	}
}
