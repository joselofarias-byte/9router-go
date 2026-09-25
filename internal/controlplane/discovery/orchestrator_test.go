package discovery

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"

	_ "modernc.org/sqlite"
)

type scriptedAdapter struct {
	id     string
	scope  []string
	rounds [][]Candidate
	err    error
	calls  int
}

func (a *scriptedAdapter) SourceID() string { return a.id }

func (a *scriptedAdapter) ScopedProviderIDs() []string { return a.scope }

func (a *scriptedAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	if a.err != nil && a.calls >= len(a.rounds) {
		a.calls++
		return nil, a.err
	}
	idx := a.calls
	a.calls++
	if idx >= len(a.rounds) {
		idx = len(a.rounds) - 1
	}
	out := make([]Candidate, len(a.rounds[idx]))
	copy(out, a.rounds[idx])
	return out, nil
}

func openDiscoveryDB(t *testing.T) *sql.DB {
	t.Helper()
	path := t.TempDir() + "/discovery.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
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
		CREATE TABLE registry_snapshots (
			version TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			reason TEXT NOT NULL,
			checksum TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO providerConnections (id, provider, authType, isActive, data, createdAt, updatedAt) VALUES (?, ?, ?, 1, '{}', ?, ?)`,
		"conn-cline", "cline", "bearer", now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	if err := registry.InitRegistry(db); err != nil {
		t.Fatalf("init registry: %v", err)
	}
	return db
}

func modelPricing(t *testing.T, provider, model string) (string, bool) {
	t.Helper()
	state := registry.GetActiveState()
	pm := state.ProviderModels[provider][model]
	if pm == nil {
		t.Fatalf("missing %s/%s", provider, model)
	}
	return pm.PricingMode, pm.IsActive
}

func TestOrchestrator_ReclassificationAndDisappearance(t *testing.T) {
	db := openDiscoveryDB(t)
	adapter := &scriptedAdapter{
		id:    "cline-free",
		scope: []string{"cline"},
		rounds: [][]Candidate{
			{
				{ProviderID: "cline", ModelID: "still-free", PricingMode: "free", UpstreamModel: "still-free"},
				{ProviderID: "cline", ModelID: "was-free", PricingMode: "free", UpstreamModel: "was-free"},
			},
			{
				{ProviderID: "cline", ModelID: "still-free", PricingMode: "free", UpstreamModel: "still-free"},
				{ProviderID: "cline", ModelID: "was-free", PricingMode: "paid", UpstreamModel: "was-free"},
			},
			{
				{ProviderID: "cline", ModelID: "still-free", PricingMode: "free", UpstreamModel: "still-free-v2"},
			},
			{},
		},
	}
	orch := NewOrchestrator(db, []Adapter{adapter})

	orch.RunSync(context.Background())
	if mode, active := modelPricing(t, "cline", "was-free"); mode != "free" || !active {
		t.Fatalf("initial classification = %s active=%v", mode, active)
	}

	orch.RunSync(context.Background())
	if mode, active := modelPricing(t, "cline", "was-free"); mode != "paid" || !active {
		t.Fatalf("reclassified model = %s active=%v, want paid and active", mode, active)
	}

	orch.RunSync(context.Background())
	if _, active := modelPricing(t, "cline", "was-free"); active {
		t.Fatal("model removed from the free catalog stayed active")
	}
	pm := registry.GetActiveState().ProviderModels["cline"]["still-free"]
	if pm.UpstreamModel != "still-free-v2" || pm.PricingMode != "free" || !pm.IsActive {
		t.Fatalf("remaining free model was not refreshed: %+v", pm)
	}

	orch.RunSync(context.Background())
	if _, active := modelPricing(t, "cline", "still-free"); active {
		t.Fatal("empty catalog left a stale free model active")
	}
}

func TestOrchestrator_UnknownDoesNotClobberConcretePricing(t *testing.T) {
	db := openDiscoveryDB(t)
	cline := &scriptedAdapter{
		id:    "cline-free",
		scope: []string{"cline"},
		rounds: [][]Candidate{{
			{ProviderID: "cline", ModelID: "shared", PricingMode: "free", UpstreamModel: "shared"},
		}},
	}
	modelsDev := &scriptedAdapter{
		id: "models.dev",
		rounds: [][]Candidate{{
			{ProviderID: "cline", ModelID: "shared", PricingMode: "unknown", UpstreamModel: "shared"},
			{ProviderID: "cline", ModelID: "only-dev", PricingMode: "unknown", UpstreamModel: "only-dev"},
		}},
	}
	NewOrchestrator(db, []Adapter{cline, modelsDev}).RunSync(context.Background())

	if mode, active := modelPricing(t, "cline", "shared"); mode != "free" || !active {
		t.Fatalf("models.dev unknown clobbered free classification: %s active=%v", mode, active)
	}
	if mode, _ := modelPricing(t, "cline", "only-dev"); mode != "unknown" {
		t.Fatalf("models.dev model = %s, want unknown", mode)
	}
}

func TestOrchestrator_SameSourceUnknownReplacesFree(t *testing.T) {
	db := openDiscoveryDB(t)
	adapter := &scriptedAdapter{
		id:    "unorouter",
		scope: []string{"unorouter"},
		rounds: [][]Candidate{
			{{ProviderID: "unorouter", ModelID: "gpt-4o:free", PricingMode: "free", UpstreamModel: "gpt-4o:free"}},
			{{ProviderID: "unorouter", ModelID: "gpt-4o:free", PricingMode: "unknown", UpstreamModel: "gpt-4o:free"}},
		},
	}
	orch := NewOrchestrator(db, []Adapter{adapter})
	orch.RunSync(context.Background())
	orch.RunSync(context.Background())
	if mode, active := modelPricing(t, "unorouter", "gpt-4o:free"); mode != "unknown" || !active {
		t.Fatalf("same-source reclassification = %s active=%v, want unknown", mode, active)
	}
}

func TestOrchestrator_FailedAdapterKeepsPreviousFreeModels(t *testing.T) {
	db := openDiscoveryDB(t)
	okAdapter := &scriptedAdapter{
		id:    "cline-free",
		scope: []string{"cline"},
		rounds: [][]Candidate{{
			{ProviderID: "cline", ModelID: "keep", PricingMode: "free", UpstreamModel: "keep"},
		}},
	}
	NewOrchestrator(db, []Adapter{okAdapter}).RunSync(context.Background())

	failed := &scriptedAdapter{id: "cline-free", scope: []string{"cline"}, err: os.ErrClosed}
	NewOrchestrator(db, []Adapter{failed}).RunSync(context.Background())
	if mode, active := modelPricing(t, "cline", "keep"); mode != "free" || !active {
		t.Fatalf("failed discovery wiped the free model: %s active=%v", mode, active)
	}
}
