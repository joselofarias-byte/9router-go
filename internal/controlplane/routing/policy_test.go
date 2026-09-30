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

func TestEngine_SelectCandidates_DynamicFreePool(t *testing.T) {
	registry.InitRegistry(nil)
	state := registry.GetActiveState()

	state.Providers["p1"] = &registry.Provider{ID: "p1", IsActive: true}
	state.Providers["p2"] = &registry.Provider{ID: "p2", IsActive: true}
	state.Providers["p_inactive"] = &registry.Provider{ID: "p_inactive", IsActive: false}

	state.ProviderModels["p1"] = map[string]*registry.ProviderModel{
		"gpt-4":   {ProviderID: "p1", ModelID: "gpt-4", PricingMode: "paid", IsActive: true},
		"llama":   {ProviderID: "p1", ModelID: "llama", UpstreamModel: "llama-3.1-8b", PricingMode: "free", IsActive: true},
		"dropped": {ProviderID: "p1", ModelID: "dropped", PricingMode: "free", IsActive: false},
	}
	state.ProviderModels["p2"] = map[string]*registry.ProviderModel{
		"qwen":    {ProviderID: "p2", ModelID: "qwen", PricingMode: "free_tier", IsActive: true},
		"unknown": {ProviderID: "p2", ModelID: "deepseek-chat-free", PricingMode: "unknown", IsActive: true},
		"upper":   {ProviderID: "p2", ModelID: "upper", PricingMode: "FREE", IsActive: true},
		"spaced":  {ProviderID: "p2", ModelID: "spaced", PricingMode: "free ", IsActive: true},
		"trial":   {ProviderID: "p2", ModelID: "trial", PricingMode: "trial", IsActive: true},
		"blank":   {ProviderID: "p2", ModelID: "blank", PricingMode: "", IsActive: true},
	}
	state.ProviderModels["p_inactive"] = map[string]*registry.ProviderModel{
		"free-but-down": {ProviderID: "p_inactive", ModelID: "free-but-down", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["p_missing"] = map[string]*registry.ProviderModel{
		"orphan": {ProviderID: "p_missing", ModelID: "orphan", PricingMode: "free", IsActive: true},
	}

	state.Accounts["a1"] = &registry.Account{ID: "a1", ProviderID: "p1", IsActive: true}
	state.Accounts["a2"] = &registry.Account{ID: "a2", ProviderID: "p2", IsActive: true}
	state.Accounts["a_inactive"] = &registry.Account{ID: "a_inactive", ProviderID: "p_inactive", IsActive: true}
	state.Accounts["a_missing"] = &registry.Account{ID: "a_missing", ProviderID: "p_missing", IsActive: true}
	state.Accounts["a1_off"] = &registry.Account{ID: "a1_off", ProviderID: "p1", IsActive: false}

	engine := &Engine{TrustManager: trust.NewManager()}

	res := engine.SelectCandidates("", PolicyFreeOnly)
	got := map[string]bool{}
	for _, node := range res {
		got[node.ProviderID+"/"+node.ModelID] = true
		if node.AccountID == "a1_off" {
			t.Errorf("inactive account %s was selected", node.AccountID)
		}
	}
	for _, want := range []string{"p1/llama", "p2/qwen"} {
		if !got[want] {
			t.Errorf("expected free pool to include %s, got %#v", want, got)
		}
	}
	for _, banned := range []string{"p1/gpt-4", "p1/dropped", "p2/deepseek-chat-free", "p2/upper", "p2/spaced", "p2/trial", "p2/blank", "p_inactive/free-but-down", "p_missing/orphan"} {
		if got[banned] {
			t.Errorf("free pool included %s", banned)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 free models, got %#v", got)
	}

	// Exact match must not expand to the rest of the pool.
	exact := engine.SelectCandidates("gpt-4", PolicyFreeOnly)
	if len(exact) != 0 {
		t.Fatalf("paid gpt-4 must stay out of free-only exact match, got %+v", exact)
	}
	exactFree := engine.SelectCandidates("llama", PolicyBalanced)
	if len(exactFree) != 1 || exactFree[0].ProviderID != "p1" || exactFree[0].UpstreamModel != "llama-3.1-8b" {
		t.Fatalf("exact llama match changed, got %+v", exactFree)
	}
}
