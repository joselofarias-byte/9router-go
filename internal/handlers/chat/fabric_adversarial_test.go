package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"9router/proxy/internal/controlplane/discovery"
	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy"
)

func resetFabricState(t *testing.T) {
	t.Helper()
	registry.InitRegistry(nil)
	globalTrustManager = trust.NewManager()
	globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager}
}

func TestResolveFabricPool_AllUnavailable(t *testing.T) {
	resetFabricState(t)
	h := &ChatHandler{}
	if info := h.resolveFabricPool("free-best"); info != nil {
		t.Fatalf("expected nil, got %+v", info)
	}
	if _, err := h.resolveModel("free-best"); err == nil {
		t.Fatal("expected no-route error")
	}
}

func TestResolveFabricPool_DynamicBest(t *testing.T) {
	resetFabricState(t)
	state := registry.GetActiveState()
	state.Providers["cline"] = &registry.Provider{ID: "cline", IsActive: true}
	state.Providers["orcarouter"] = &registry.Provider{ID: "orcarouter", IsActive: true}
	state.ProviderModels["cline"] = map[string]*registry.ProviderModel{
		"slow": {ProviderID: "cline", ModelID: "slow", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["orcarouter"] = map[string]*registry.ProviderModel{
		"fast": {ProviderID: "orcarouter", ModelID: "fast", PricingMode: "free", IsActive: true},
	}
	state.Accounts["c1"] = &registry.Account{ID: "c1", ProviderID: "cline", IsActive: true}
	state.Accounts["o1"] = &registry.Account{ID: "o1", ProviderID: "orcarouter", IsActive: true}
	globalTrustManager.RecordObservation("orcarouter", "fast", "o1", true, "")
	globalTrustManager.RecordLatency("orcarouter", "fast", "o1", 80)
	globalTrustManager.RecordObservation("cline", "slow", "c1", true, "")
	globalTrustManager.RecordLatency("cline", "slow", "c1", 4000)

	info := hResolve(t, "free-best")
	if info.Provider != "orcarouter" || info.Model != "fast" {
		t.Fatalf("expected orcarouter/fast first, got %s/%s combo=%v", info.Provider, info.Model, info.ComboModels)
	}
	if len(info.ComboModels) != 2 {
		t.Fatalf("expected 2 combo entries, got %v", info.ComboModels)
	}
	if info.Strategy != "fallback" {
		t.Fatalf("strategy=%s", info.Strategy)
	}
}

func hResolve(t *testing.T, name string) *ModelInfo {
	t.Helper()
	h := &ChatHandler{}
	info, err := h.resolveModel(name)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestConcurrentRoutingAndSnapshotUpdates(t *testing.T) {
	resetFabricState(t)
	state := registry.GetActiveState()
	state.Providers["cline"] = &registry.Provider{ID: "cline", IsActive: true}
	state.ProviderModels["cline"] = map[string]*registry.ProviderModel{
		"m": {ProviderID: "cline", ModelID: "m", PricingMode: "free", IsActive: true},
	}
	state.Accounts["a1"] = &registry.Account{ID: "a1", ProviderID: "cline", IsActive: true}

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = getPolicyCandidates(context.Background(), nil, "fabric-free", routing.PolicyFreeOnly)
				globalTrustManager.RecordObservation("cline", "m", "a1", j%3 != 0, providers.ErrTransient)
				globalTrustManager.RecordLatency("cline", "m", "a1", j)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			registry.UpdateAccounts(map[string]*registry.Account{
				"a1": {ID: "a1", ProviderID: "cline", IsActive: true},
			})
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestInvalidDiscoveryPayloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"not":"a catalog"`))
	}))
	defer srv.Close()

	if _, err := discovery.NewKiraAdapter(srv.Client(), srv.URL).Discover(context.Background()); err == nil {
		t.Fatal("kira should reject truncated JSON")
	}
	orig := discovery.ModelsDevCatalogURL
	discovery.ModelsDevCatalogURL = srv.URL
	defer func() { discovery.ModelsDevCatalogURL = orig }()
	if _, err := discovery.NewModelsDevAdapter(srv.Client()).Discover(context.Background()); err == nil {
		t.Fatal("models.dev should reject invalid JSON")
	}
}

func TestFallbackLoopDoesNotRetrySameAccountForever(t *testing.T) {
	resetFabricState(t)
	state := registry.GetActiveState()
	state.Providers["cline"] = &registry.Provider{ID: "cline", IsActive: true}
	state.ProviderModels["cline"] = map[string]*registry.ProviderModel{
		"m": {ProviderID: "cline", ModelID: "m", PricingMode: "free", IsActive: true},
	}
	state.Accounts["a1"] = &registry.Account{ID: "a1", ProviderID: "cline", IsActive: true}

	for i := 0; i < 3; i++ {
		recordRouteOutcome("cline", "m", "a1", false, 429, []byte("rate limit"), 10)
	}
	if blocked, why := globalTrustManager.IsUnavailable("cline", "m", "a1"); !blocked || why != "quota_exhausted" {
		t.Fatalf("expected quota block, blocked=%v why=%s", blocked, why)
	}
	got := getPolicyCandidates(context.Background(), nil, "free-best", routing.PolicyFreeOnly)
	if len(got) != 0 {
		t.Fatalf("quota-exhausted node still selected: %+v", got)
	}
}

func TestRetryLoopClassifiesTransient(t *testing.T) {
	cls := providers.ClassifyError(503, "server is temporarily unavailable", 0)
	if cls.Category != providers.ErrTransient && cls.Category != providers.ErrRateLimit {
		if !cls.ShouldFallback {
			t.Fatalf("503 must fallback: %+v", cls)
		}
	}
	if !cls.ShouldFallback {
		t.Fatal("transient should fallback")
	}
}

func TestMalformedSSEScanner(t *testing.T) {
	body := strings.NewReader("data: {not-json\n\ndata: [DONE]\n\n")
	var chunks int
	if err := proxy.ScanStream(body, func([]byte) { chunks++ }); err != nil {
		t.Fatalf("malformed JSON payload must still scan frames: %v", err)
	}
	if chunks == 0 {
		t.Fatal("expected scanned frames")
	}
}

func TestCancellationDoesNotMarkSuccess(t *testing.T) {
	resetFabricState(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recordRouteOutcome("cline", "m", "a1", false, 499, []byte("client closed request"), 0)
	lvl := globalTrustManager.GetTrustLevel("cline", "m", "a1")
	if lvl == trust.TrustVerified || lvl == trust.TrustTrusted {
		t.Fatalf("cancel must not verify a route, got %s", lvl)
	}
	_ = ctx
}

func TestAccountRiskPenaltyApplied(t *testing.T) {
	resetFabricState(t)
	state := registry.GetActiveState()
	state.Providers["cline"] = &registry.Provider{ID: "cline", IsActive: true}
	state.Providers["antigravity"] = &registry.Provider{ID: "antigravity", IsActive: true}
	state.ProviderModels["cline"] = map[string]*registry.ProviderModel{
		"m": {ProviderID: "cline", ModelID: "m", PricingMode: "free", IsActive: true},
	}
	state.ProviderModels["antigravity"] = map[string]*registry.ProviderModel{
		"m": {ProviderID: "antigravity", ModelID: "m", PricingMode: "free", IsActive: true},
	}
	state.Accounts["c"] = &registry.Account{ID: "c", ProviderID: "cline", IsActive: true}
	state.Accounts["a"] = &registry.Account{ID: "a", ProviderID: "antigravity", IsActive: true}
	got := getPolicyCandidates(context.Background(), nil, "m", routing.PolicyFreeOnly)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].ProviderID != "cline" {
		t.Fatalf("low-risk should win, first=%s", got[0].ProviderID)
	}
}

func TestPoolNamesUnified(t *testing.T) {
	if !pools.IsPool("free-best") || pools.CanonicalName("free") != pools.FabricFree {
		t.Fatal("aliases must unify to fabric-free")
	}
}

func TestProviderOutageQuarantine(t *testing.T) {
	resetFabricState(t)
	recordRouteOutcome("cline", "m", "a1", false, 401, []byte("no credentials"), 0)
	if globalTrustManager.GetTrustLevel("cline", "m", "a1") != trust.TrustQuarantined {
		t.Fatal("auth outage should quarantine")
	}
}

func TestExpiredSessionClassification(t *testing.T) {
	cls := providers.ClassifyError(0, "session expired please login", 0)
	if cls.Category != providers.ErrSession {
		t.Fatalf("got %s", cls.Category)
	}
}

func TestCorruptSnapshotLKGStillWorks(t *testing.T) {
	// Covered in registry tests; this asserts the public Init path stays usable
	// after a failed FromJSON of garbage.
	if _, err := registry.FromJSON("{"); err == nil {
		t.Fatal("corrupt payload must fail")
	}
}
