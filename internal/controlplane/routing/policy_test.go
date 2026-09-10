package routing

import (
	"testing"

	"9router/proxy/internal/controlplane/pools"
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

// TestEngine_SelectCandidates_FabricFreePoolExpansion verifies that requesting
// the fabric-free logical pool expands across heterogeneous providers/models
// (different ModelIDs entirely) instead of requiring an exact ModelID match,
// and that no paid route ever leaks into the pool.
func TestEngine_SelectCandidates_FabricFreePoolExpansion(t *testing.T) {
	registry.InitRegistry(nil)
	state := registry.GetActiveState()

	state.Providers["alpha"] = &registry.Provider{ID: "alpha", IsActive: true}
	state.Providers["beta"] = &registry.Provider{ID: "beta", IsActive: true}
	state.Providers["gamma"] = &registry.Provider{ID: "gamma", IsActive: true}

	// alpha/model-x and beta/model-y are different model families entirely —
	// this is exactly the heterogeneous case pools must support.
	state.ProviderModels["alpha"] = map[string]*registry.ProviderModel{
		"model-x": {ProviderID: "alpha", ModelID: "model-x", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["beta"] = map[string]*registry.ProviderModel{
		"model-y": {ProviderID: "beta", ModelID: "model-y", PricingMode: "free_tier", IsActive: true},
	}
	// gamma is a paid provider and must never appear in fabric-free.
	state.ProviderModels["gamma"] = map[string]*registry.ProviderModel{
		"model-z": {ProviderID: "gamma", ModelID: "model-z", PricingMode: "paid", IsActive: true},
	}

	state.Accounts["a1"] = &registry.Account{ID: "a1", ProviderID: "alpha", IsActive: true}
	state.Accounts["b1"] = &registry.Account{ID: "b1", ProviderID: "beta", IsActive: true}
	state.Accounts["g1"] = &registry.Account{ID: "g1", ProviderID: "gamma", IsActive: true}

	engine := &Engine{TrustManager: trust.NewManager()}
	res := engine.SelectCandidates(pools.FabricFree, PolicyFreeOnly)

	if len(res) != 2 {
		t.Fatalf("expected 2 heterogeneous free candidates, got %d: %+v", len(res), res)
	}
	seen := map[string]bool{}
	for _, n := range res {
		seen[n.ProviderID+"/"+n.ModelID] = true
		if n.ProviderID == "gamma" {
			t.Fatalf("paid provider gamma leaked into fabric-free pool")
		}
	}
	if !seen["alpha/model-x"] || !seen["beta/model-y"] {
		t.Fatalf("expected both alpha/model-x and beta/model-y in pool, got %v", res)
	}
}

// TestEngine_SelectCandidates_FabricFreeExcludesUncredentialedProvider verifies
// a free provider with no active account is not offered as a candidate — Fabric
// must not select a route it cannot actually authenticate against.
func TestEngine_SelectCandidates_FabricFreeExcludesUncredentialedProvider(t *testing.T) {
	registry.InitRegistry(nil)
	state := registry.GetActiveState()

	state.Providers["nocreds"] = &registry.Provider{ID: "nocreds", IsActive: true}
	state.ProviderModels["nocreds"] = map[string]*registry.ProviderModel{
		"free-model": {ProviderID: "nocreds", ModelID: "free-model", PricingMode: "free", IsActive: true},
	}
	// Deliberately no accounts for "nocreds".

	engine := &Engine{TrustManager: trust.NewManager()}
	res := engine.SelectCandidates(pools.FabricFree, PolicyFreeOnly)
	if len(res) != 0 {
		t.Fatalf("expected 0 candidates for uncredentialed free provider, got %d: %+v", len(res), res)
	}
}

// TestEngine_SelectCandidates_AccountRiskOrdering verifies that when two free
// candidates are otherwise equivalent, the ordinary API-key provider outranks
// a risky account/product-surface provider (here "antigravity", which carries
// a real penalty in providers.GetProviderRiskProfile) — without excluding the
// risky one entirely, since it must remain usable as a fallback.
func TestEngine_SelectCandidates_AccountRiskOrdering(t *testing.T) {
	registry.InitRegistry(nil)
	state := registry.GetActiveState()

	state.Providers["safe-api"] = &registry.Provider{ID: "safe-api", IsActive: true}
	state.Providers["antigravity"] = &registry.Provider{ID: "antigravity", IsActive: true}

	state.ProviderModels["safe-api"] = map[string]*registry.ProviderModel{
		"m1": {ProviderID: "safe-api", ModelID: "m1", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["antigravity"] = map[string]*registry.ProviderModel{
		"m2": {ProviderID: "antigravity", ModelID: "m2", PricingMode: "free", IsActive: true},
	}

	state.Accounts["s1"] = &registry.Account{ID: "s1", ProviderID: "safe-api", IsActive: true}
	state.Accounts["ag1"] = &registry.Account{ID: "ag1", ProviderID: "antigravity", IsActive: true}

	engine := &Engine{TrustManager: trust.NewManager()}
	res := engine.SelectCandidates(pools.FabricFree, PolicyFreeOnly)
	if len(res) != 2 {
		t.Fatalf("expected both candidates present (risky one is a fallback, not excluded), got %d: %+v", len(res), res)
	}
	if res[0].ProviderID != "safe-api" {
		t.Errorf("expected safe-api to outrank antigravity when otherwise equivalent, got order %+v", res)
	}
}
