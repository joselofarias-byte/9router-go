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

	state.Providers["p_inactive"] = &registry.Provider{ID: "p_inactive", IsActive: false}
	// Missing provider test case: state.Providers doesn't have "p_missing"

	state.ProviderModels["p1"] = map[string]*registry.ProviderModel{
		"gpt-4":      {ProviderID: "p1", ModelID: "gpt-4", PricingMode: "paid", IsActive: true},
		"gpt-4-drop": {ProviderID: "p1", ModelID: "gpt-4", PricingMode: "paid", IsActive: false}, // Should be excluded
	}
	state.ProviderModels["p2"] = map[string]*registry.ProviderModel{
		"gpt-4": {ProviderID: "p2", ModelID: "gpt-4", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["p_inactive"] = map[string]*registry.ProviderModel{
		"gpt-4": {ProviderID: "p_inactive", ModelID: "gpt-4", PricingMode: "paid", IsActive: true}, // Should be excluded because provider is inactive
	}
	state.ProviderModels["p_missing"] = map[string]*registry.ProviderModel{
		"gpt-4": {ProviderID: "p_missing", ModelID: "gpt-4", PricingMode: "paid", IsActive: true}, // Should be excluded because provider is missing
	}

	state.Accounts["a1"] = &registry.Account{ID: "a1", ProviderID: "p1", IsActive: true}
	state.Accounts["a2"] = &registry.Account{ID: "a2", ProviderID: "p2", IsActive: true}
	state.Accounts["a_inactive"] = &registry.Account{ID: "a_inactive", ProviderID: "p_inactive", IsActive: true}
	state.Accounts["a_missing"] = &registry.Account{ID: "a_missing", ProviderID: "p_missing", IsActive: true}

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


func TestEngine_SelectCandidates_UsesObservedPerformance(t *testing.T) {
	registry.InitRegistry(nil)
	state := registry.GetActiveState()

	for _, providerID := range []string{"fast", "slow"} {
		state.Providers[providerID] = &registry.Provider{ID: providerID, IsActive: true}
		state.ProviderModels[providerID] = map[string]*registry.ProviderModel{
			"model": {ProviderID: providerID, ModelID: "model", PricingMode: "paid", IsActive: true},
		}
		state.Accounts[providerID+"-account"] = &registry.Account{
			ID: providerID + "-account", ProviderID: providerID, IsActive: true,
		}
	}

	tm := trust.NewManager()
	// Keep trust and success rate equal; TTFT is the only scoring difference.
	tm.RecordRequestOutcome("fast", "model", "fast-account", true, "", 150, 100)
	tm.RecordRequestOutcome("slow", "model", "slow-account", true, "", 150, 2500)

	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("model", PolicyBalanced)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
	if got[0].ProviderID != "fast" {
		t.Fatalf("expected lower observed TTFT to rank first, got %#v", got)
	}
	if got[0].Score.Factors.TTFTMs != 100 || got[1].Score.Factors.TTFTMs != 2500 {
		t.Fatalf("routing did not consume observed TTFT: %#v", got)
	}
}
