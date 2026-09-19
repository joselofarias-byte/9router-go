package routing

import (
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
)

func seedFreeRegistry(t *testing.T) *trust.Manager {
	t.Helper()
	registry.InitRegistry(nil)
	state := registry.GetActiveState()
	state.Providers["cline"] = &registry.Provider{ID: "cline", IsActive: true}
	state.Providers["orcarouter"] = &registry.Provider{ID: "orcarouter", IsActive: true}
	state.Providers["paid"] = &registry.Provider{ID: "paid", IsActive: true}
	state.ProviderModels["cline"] = map[string]*registry.ProviderModel{
		"free-a": {ProviderID: "cline", ModelID: "free-a", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["orcarouter"] = map[string]*registry.ProviderModel{
		"free-b": {ProviderID: "orcarouter", ModelID: "free-b", PricingMode: "free_tier", IsActive: true},
	}
	state.ProviderModels["paid"] = map[string]*registry.ProviderModel{
		"gpt-4": {ProviderID: "paid", ModelID: "gpt-4", PricingMode: "paid", IsActive: true},
	}
	state.Accounts["c1"] = &registry.Account{ID: "c1", ProviderID: "cline", IsActive: true}
	state.Accounts["o1"] = &registry.Account{ID: "o1", ProviderID: "orcarouter", IsActive: true}
	state.Accounts["p1"] = &registry.Account{ID: "p1", ProviderID: "paid", IsActive: true}
	return trust.NewManager()
}

func TestSelectCandidates_FreePoolAliases(t *testing.T) {
	tm := seedFreeRegistry(t)
	engine := &Engine{TrustManager: tm}
	for _, name := range []string{"fabric-free", "free-best", "free"} {
		got := engine.SelectCandidates(name, PolicyFreeOnly)
		if len(got) != 2 {
			t.Fatalf("%s: expected 2 free candidates, got %d", name, len(got))
		}
		for _, n := range got {
			if n.ProviderID == "paid" {
				t.Fatalf("%s leaked paid provider", name)
			}
		}
	}
}

func TestSelectCandidates_EmptyModelDynamicPool(t *testing.T) {
	tm := seedFreeRegistry(t)
	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("", PolicyFreeOnly)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestSelectCandidates_QuotaSkip(t *testing.T) {
	tm := seedFreeRegistry(t)
	tm.RecordQuota("cline", "free-a", "c1", time.Hour)
	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("fabric-free", PolicyFreeOnly)
	if len(got) != 1 || got[0].ProviderID != "orcarouter" {
		t.Fatalf("quota skip failed: %+v", got)
	}
}

func TestSelectCandidates_ExpiredSession(t *testing.T) {
	tm := seedFreeRegistry(t)
	tm.RecordSessionExpired("orcarouter", "free-b", "o1")
	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("free-best", PolicyFreeOnly)
	if len(got) != 1 || got[0].ProviderID != "cline" {
		t.Fatalf("session skip failed: %+v", got)
	}
}

func TestSelectCandidates_AccountRiskOrders(t *testing.T) {
	tm := seedFreeRegistry(t)
	// antigravity-style penalty should lose to ordinary API-key when health is equal
	state := registry.GetActiveState()
	state.Providers["antigravity"] = &registry.Provider{ID: "antigravity", IsActive: true}
	state.ProviderModels["antigravity"] = map[string]*registry.ProviderModel{
		"free-c": {ProviderID: "antigravity", ModelID: "free-c", PricingMode: "free", IsActive: true},
	}
	state.Accounts["ag1"] = &registry.Account{ID: "ag1", ProviderID: "antigravity", IsActive: true}
	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("fabric-free", PolicyFreeOnly)
	if len(got) < 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if got[len(got)-1].ProviderID != "antigravity" {
		t.Fatalf("high-risk should sort last, last=%s scores=%v", got[len(got)-1].ProviderID, []float64{got[0].Score.Total, got[1].Score.Total, got[2].Score.Total})
	}
}

func TestSelectCandidates_AllFreeUnavailable(t *testing.T) {
	tm := seedFreeRegistry(t)
	tm.RecordSessionExpired("cline", "free-a", "c1")
	tm.RecordQuota("orcarouter", "free-b", "o1", time.Hour)
	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("free-best", PolicyFreeOnly)
	if len(got) != 0 {
		t.Fatalf("expected empty pool, got %+v", got)
	}
}

func TestSelectCandidates_RecentLatencyStillRoutable(t *testing.T) {
	tm := seedFreeRegistry(t)
	tm.RecordLatency("cline", "free-a", "c1", 50)
	if _, ok := tm.LatencyStats("cline", "free-a", "c1"); !ok {
		t.Fatal("fresh latency should be visible")
	}
	engine := &Engine{TrustManager: tm}
	got := engine.SelectCandidates("fabric-free", PolicyFreeOnly)
	if len(got) != 2 {
		t.Fatalf("latency must not drop candidates, got %d", len(got))
	}
}
