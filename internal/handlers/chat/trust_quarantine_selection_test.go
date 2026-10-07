package chat

import (
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func setupQuarantinedSelectionTest(t *testing.T) (*ChatHandler, string, string, func()) {
	t.Helper()
	const provider = "deepseek"
	const model = "audit-quarantine-model"
	const connID = "conn-trust-quarantined"

	database, cleanupDB := setupChatTestDB(t)
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = ?`, provider); err != nil {
		cleanupDB()
		t.Fatalf("clear provider connections: %v", err)
	}
	seedConnDB(t, database, provider, connID, "sk-audit", "http://127.0.0.1:1")

	if err := registry.InitRegistry(nil); err != nil {
		cleanupDB()
		t.Fatalf("init registry: %v", err)
	}
	state := registry.GetActiveState()
	state.Providers[provider] = &registry.Provider{ID: provider, IsActive: true}
	state.Models[model] = &registry.Model{ID: model, Name: model}
	state.ProviderModels[provider] = map[string]*registry.ProviderModel{
		model: {
			ProviderID: provider,
			ModelID: model,
			UpstreamModel: model,
			PricingMode: "paid",
			IsActive: true,
		},
	}

	oldTM := globalTrustManager
	oldEngine := globalRoutingEngine
	tm := trust.NewManager()
	globalTrustManager = tm
	globalRoutingEngine = &routing.Engine{TrustManager: tm}
	tm.RecordObservation(provider, model, connID, false, providers.ErrAuth)

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)
	cleanup := func() {
		globalTrustManager = oldTM
		globalRoutingEngine = oldEngine
		cleanupDB()
	}
	return h, model, connID, cleanup
}

func TestGetBestConnection_DoesNotLegacyFallbackIntoTrustQuarantine(t *testing.T) {
	h, model, _, cleanup := setupQuarantinedSelectionTest(t)
	defer cleanup()

	if _, _, err := h.getBestConnection("deepseek", "", nil, model); err == nil {
		t.Fatal("expected quarantined account to remain unavailable after Control Plane returns no candidates")
	}
}

func TestGetBestConnection_PinnedIDDoesNotBypassTrustQuarantine(t *testing.T) {
	h, model, connID, cleanup := setupQuarantinedSelectionTest(t)
	defer cleanup()

	if _, _, err := h.getBestConnection("deepseek", connID, nil, model); err == nil {
		t.Fatal("expected pinned connection to respect trust quarantine")
	}
}


func TestAccountTrustSelectable_AllowsDegradedState(t *testing.T) {
	oldTM := globalTrustManager
	tm := trust.NewManager()
	globalTrustManager = tm
	defer func() { globalTrustManager = oldTM }()

	const provider = "deepseek"
	const model = "audit-degraded-model"
	const account = "degraded-account"

	// One success establishes a verified record; a transient failure degrades it
	// without quarantining it.
	tm.RecordObservation(provider, model, account, true, "")
	tm.RecordObservation(provider, model, account, false, providers.ErrTransient)

	if level := tm.GetTrustLevel(provider, model, account); level != trust.TrustDegraded {
		t.Fatalf("expected degraded trust, got %s", level)
	}
	if !isAccountTrustSelectable(provider, model, account) {
		t.Fatal("degraded account should remain selectable")
	}
}
