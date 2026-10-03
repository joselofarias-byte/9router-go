package routing

import (
	"testing"
	"time"

	"9router/proxy/internal/controlplane/capacity"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
)

func TestQuotaBridgePreservesCatalogAndNeverAdmitsPaid(t *testing.T) {
	capacity.ClearAll()
	t.Cleanup(capacity.ClearAll)
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	state := registry.GetActiveState()
	state.Providers["p"] = &registry.Provider{ID: "p", IsActive: true}
	state.ProviderModels["p"] = map[string]*registry.ProviderModel{
		"catalog": {ProviderID: "p", ModelID: "catalog", UpstreamModel: "wire/full:model", PricingMode: "free_tier", IsActive: true},
		"paid":    {ProviderID: "p", ModelID: "paid", UpstreamModel: "paid-wire", PricingMode: "paid", IsActive: true},
	}
	for _, id := range []string{"a", "b"} {
		state.Accounts[id] = &registry.Account{ID: id, ProviderID: "p", IsActive: true}
	}
	capacity.Update("p", "a", "wire/full:model", 0, time.Now().Add(time.Hour))
	capacity.Update("p", "b", "wire/full:model", 50, time.Now().Add(time.Hour))
	engine := &Engine{TrustManager: trust.NewManager()}
	nodes := engine.SelectCandidates("", PolicyFreeOnly)
	if len(nodes) != 1 || nodes[0].AccountID != "b" || nodes[0].ModelID != "catalog" || nodes[0].UpstreamModel != "wire/full:model" || nodes[0].Quota.Status != capacity.Available {
		t.Fatalf("identity/quota: %+v", nodes)
	}
	capacity.Update("p", "b", "wire/full:model", 50, time.Now().Add(-time.Minute))
	nodes = engine.SelectCandidates("", PolicyFreeOnly)
	if len(nodes) != 1 || nodes[0].Quota.Status != capacity.Stale {
		t.Fatalf("reset invented replenishment: %+v", nodes)
	}
	scope := capacity.Scope{Project: "fixture-project"}
	capacity.SetScope("p", "a", scope)
	capacity.SetScope("p", "b", scope)
	capacity.Update("p", "a", "wire/full:model", 0, time.Now().Add(time.Hour))
	if nodes = engine.SelectCandidates("", PolicyFreeOnly); len(nodes) != 0 {
		t.Fatalf("shared quota bypass: %+v", nodes)
	}
	capacity.Update("p", "b", "wire/full:model", 75, time.Now().Add(time.Hour))
	if nodes = engine.SelectCandidates("", PolicyFreeOnly); len(nodes) != 2 {
		t.Fatalf("reported recovery absent: %+v", nodes)
	}
}
