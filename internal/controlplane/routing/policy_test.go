package routing

import (
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
)

func TestEngine_SelectCandidates(t *testing.T) {
	// Setup mock state manually
	registry.InitRegistry(nil) // Init empty state

	state := registry.GetActiveState()

	state.Providers["p1"] = &registry.Provider{ID: "p1", IsActive: true}
	state.Providers["p2"] = &registry.Provider{ID: "p2", IsActive: true}

	state.ProviderModels["p1"] = map[string]*registry.ProviderModel{
		"gpt-4": {ProviderID: "p1", ModelID: "gpt-4", PricingMode: "paid", IsActive: true},
	}
	state.ProviderModels["p2"] = map[string]*registry.ProviderModel{
		"gpt-4": {ProviderID: "p2", ModelID: "gpt-4", PricingMode: "free", IsActive: true},
	}

	state.Accounts["a1"] = &registry.Account{ID: "a1", ProviderID: "p1", IsActive: true}
	state.Accounts["a2"] = &registry.Account{ID: "a2", ProviderID: "p2", IsActive: true}

	tm := trust.NewManager()
	// Trust p1 more than p2
	tm.RecordObservation("p1", "gpt-4", "a1", true, "")
	tm.RecordObservation("p1", "gpt-4", "a1", true, "") // p1 is Verified

	engine := &Engine{TrustManager: tm}

	// 1. Balanced Policy
	res := engine.SelectCandidates("gpt-4", PolicyBalanced)
	if len(res) != 2 {
		t.Fatalf("Expected 2 candidates, got %d", len(res))
	}

	// 2. Free Only
	resFree := engine.SelectCandidates("gpt-4", PolicyFreeOnly)
	if len(resFree) != 1 || resFree[0].ProviderID != "p2" {
		t.Fatalf("Expected 1 free candidate (p2), got %v", resFree)
	}

	// 3. Free First
	resFirst := engine.SelectCandidates("gpt-4", PolicyFreeFirst)
	if len(resFirst) != 2 {
		t.Fatalf("Expected 2 candidates, got %d", len(resFirst))
	}
	if resFirst[0].ProviderID != "p2" {
		t.Errorf("Expected p2 to be first due to free-first bonus, got %s", resFirst[0].ProviderID)
	}
}
