package discovery

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	_ "modernc.org/sqlite"
)

type mutableDiscoveryAdapter struct {
	id         string
	candidates []Candidate
	err        error
}

func (a *mutableDiscoveryAdapter) SourceID() string { return a.id }

func (a *mutableDiscoveryAdapter) Discover(context.Context) ([]Candidate, error) {
	if a.err != nil {
		return nil, a.err
	}
	return append([]Candidate(nil), a.candidates...), nil
}

func setupOrchestratorDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "orchestrator.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })

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
	`); err != nil {
		t.Fatalf("create orchestrator tables: %v", err)
	}
	if err := registry.InitRegistry(database); err != nil {
		t.Fatalf("init registry: %v", err)
	}
	return database
}

func TestOrchestratorRefreshesMutableOfferMetadataAndRetiresMissingOffers(t *testing.T) {
	database := setupOrchestratorDB(t)
	adapter := &mutableDiscoveryAdapter{
		id: "test-source",
		candidates: []Candidate{{
			ProviderID:    "orcarouter",
			ModelID:       "model-a",
			UpstreamModel: "model-a",
			PricingMode:   "free_tier",
			CostMetadata:  `{"prompt":0}`,
			Capabilities:  `{"vision":false}`,
		}},
	}
	orchestrator := NewOrchestrator(database, []Adapter{adapter})

	orchestrator.RunSync(context.Background())
	state := registry.GetActiveState()
	pm := state.ProviderModels["orcarouter"]["model-a"]
	if pm == nil || !pm.IsActive || pm.PricingMode != "free_tier" {
		t.Fatalf("expected initial active free offer, got %#v", pm)
	}

	adapter.candidates[0].UpstreamModel = "model-a-v2"
	adapter.candidates[0].PricingMode = "paid"
	adapter.candidates[0].CostMetadata = `{"prompt":0.01}`
	adapter.candidates[0].Capabilities = `{"vision":true}`
	orchestrator.RunSync(context.Background())

	state = registry.GetActiveState()
	pm = state.ProviderModels["orcarouter"]["model-a"]
	if pm == nil {
		t.Fatal("updated offer missing")
	}
	if pm.PricingMode != "paid" {
		t.Fatalf("expected refreshed paid pricing, got %q", pm.PricingMode)
	}
	if pm.UpstreamModel != "model-a-v2" {
		t.Fatalf("expected refreshed upstream model, got %q", pm.UpstreamModel)
	}
	if pm.CostMetadata != `{"prompt":0.01}` || pm.Capabilities != `{"vision":true}` {
		t.Fatalf("expected refreshed metadata, got cost=%q caps=%q", pm.CostMetadata, pm.Capabilities)
	}
	if !pm.IsActive {
		t.Fatal("refreshed offer should remain active")
	}

	adapter.candidates = nil
	orchestrator.RunSync(context.Background())

	state = registry.GetActiveState()
	pm = state.ProviderModels["orcarouter"]["model-a"]
	if pm == nil {
		t.Fatal("retired offer should remain explainable in registry")
	}
	if pm.IsActive {
		t.Fatal("offer missing from a fully successful discovery pass should be inactive")
	}
}

func TestOrchestratorPreservesUnseenOffersWhenAdapterFails(t *testing.T) {
	database := setupOrchestratorDB(t)
	adapter := &mutableDiscoveryAdapter{
		id: "test-source",
		candidates: []Candidate{{
			ProviderID:    "orcarouter",
			ModelID:       "model-a",
			UpstreamModel: "model-a",
			PricingMode:   "free_tier",
			Capabilities:  "{}",
		}},
	}
	orchestrator := NewOrchestrator(database, []Adapter{adapter})
	orchestrator.RunSync(context.Background())

	adapter.candidates = nil
	adapter.err = errors.New("temporary catalog outage")
	orchestrator.RunSync(context.Background())

	state := registry.GetActiveState()
	pm := state.ProviderModels["orcarouter"]["model-a"]
	if pm == nil || !pm.IsActive {
		t.Fatalf("failed adapter must preserve last known offer, got %#v", pm)
	}
	if pm.PricingMode != "free_tier" {
		t.Fatalf("failed adapter unexpectedly changed pricing: %q", pm.PricingMode)
	}
}
