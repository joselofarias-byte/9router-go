package routing

import (
	"testing"
	"time"

	"9router/proxy/internal/controlplane/availability"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
)

func TestProfilesExcludePaidAndUnknownCapabilities(t *testing.T) {
	state := &registry.RegistryState{ProviderModels: map[string]map[string]*registry.ProviderModel{
		"openrouter": {
			"coder":   {PricingMode: "free_tier", Capabilities: `{"tools":true,"reasoning":true,"context_window":262144}`},
			"unknown": {PricingMode: "free_tier", Capabilities: `{}`},
			"paid":    {PricingMode: "paid", Capabilities: `{"tools":true,"reasoning":true,"context_window":1000000}`},
		},
		"llamacpp": {"qwen": {PricingMode: "free", Capabilities: `{"local":true}`}},
	}}
	nodes := []RouteNode{
		{ProviderID: "openrouter", ModelID: "coder", LatencyMs: 50},
		{ProviderID: "openrouter", ModelID: "unknown", LatencyMs: 10},
		{ProviderID: "openrouter", ModelID: "paid"},
		{ProviderID: "llamacpp", ModelID: "qwen"},
	}
	for _, profile := range []string{"coding-best-free", "reasoning-free", "long-context", "long-context-free"} {
		got := FilterProfile(state, nodes, profile)
		if len(got) != 1 || got[0].ModelID != "coder" {
			t.Fatalf("%s: %+v", profile, got)
		}
	}
	if got := FilterProfile(state, nodes, "local"); len(got) != 1 || got[0].ProviderID != "llamacpp" {
		t.Fatalf("local: %+v", got)
	}
	if got := FilterProfile(state, nodes, "fast-free"); len(got) != 3 || got[0].ModelID != "unknown" || got[2].ModelID != "qwen" {
		t.Fatalf("fast: %+v", got)
	}
	if got := FilterProfile(state, nodes, "paid-fallback"); len(got) != 0 {
		t.Fatal("paid fallback invented implicitly")
	}
	kept := ApplyDeclaredProfile(state, nodes, "coding-best-free")
	if len(kept) != 2 {
		t.Fatalf("empty capability document was treated as a contradiction: %+v", kept)
	}
	state.ProviderModels["openrouter"]["unknown"].Capabilities = `{"tools":false}`
	kept = ApplyDeclaredProfile(state, nodes, "coding-best-free")
	if len(kept) != 1 || kept[0].ModelID != "coder" {
		t.Fatalf("declared tools:false stayed in coding profile: %+v", kept)
	}
	if got := ApplyDeclaredProfile(state, nodes, "paid-fallback"); len(got) != 0 {
		t.Fatal("declared profile invented a paid fallback")
	}
}

func TestEngineSkipsObservedCooldownAndUsesRealLatency(t *testing.T) {
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	state := registry.GetActiveState()
	state.Providers["llamacpp"] = &registry.Provider{ID: "llamacpp", IsActive: true}
	state.ProviderModels["llamacpp"] = map[string]*registry.ProviderModel{
		"qwen": {ProviderID: "llamacpp", ModelID: "qwen", PricingMode: "free", IsActive: true},
	}
	s := availability.NewStore()
	e := &Engine{TrustManager: trust.NewManager(), Availability: s}
	key := availability.Key{Provider: "llamacpp", Model: "qwen", Account: VirtualNoAuthAccountID("llamacpp")}
	s.Observe(key, true, "", 0, 80, 20)
	if got := e.SelectCandidates("", PolicyFreeOnly); len(got) != 1 || got[0].LatencyMs != 80 {
		t.Fatalf("latency absent: %+v", got)
	}
	s.Observe(key, false, "quota", time.Minute, 0, 0)
	if got := e.SelectCandidates("", PolicyFreeOnly); len(got) != 0 {
		t.Fatalf("blocked route selected: %+v", got)
	}
}
