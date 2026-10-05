package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/controlplane/availability"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func isolateFreeHealth(t *testing.T) {
	t.Helper()
	oldTrust, oldAvailability, oldEngine := globalTrustManager, globalAvailability, globalRoutingEngine
	globalTrustManager, globalAvailability = trust.NewManager(), availability.NewStore()
	globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager, Availability: globalAvailability}
	t.Cleanup(func() {
		globalTrustManager, globalAvailability, globalRoutingEngine = oldTrust, oldAvailability, oldEngine
	})
}

func TestFreePoolActual429FallsBackAndNextRequestSkipsExhaustedModel(t *testing.T) {
	isolateFreeHealth(t)
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	var exhaustedHits, healthyHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("no-auth provider leaked credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Model == "exhausted-wire" {
			exhaustedHits.Add(1)
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
			return
		}
		healthyHits.Add(1)
		_, _ = w.Write([]byte(`{"id":"ok","model":"qwen-wire","choices":[{"message":{"role":"assistant","content":"func add(a, b int) int { return a + b }"}}]}`))
	}))
	defer srv.Close()
	original := providers.KnownProviders["llamacpp"]
	local := original
	local.BaseURL = srv.URL
	providers.KnownProviders["llamacpp"] = local
	t.Cleanup(func() { providers.KnownProviders["llamacpp"] = original })
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	state := registry.GetActiveState()
	state.Providers["llamacpp"] = &registry.Provider{ID: "llamacpp", IsActive: true}
	state.ProviderModels["llamacpp"] = map[string]*registry.ProviderModel{
		"exhausted-catalog": {ProviderID: "llamacpp", ModelID: "exhausted-catalog", UpstreamModel: "exhausted-wire", PricingMode: "free", IsActive: true},
		"healthy-catalog":   {ProviderID: "llamacpp", ModelID: "healthy-catalog", UpstreamModel: "qwen-wire", PricingMode: "free", IsActive: true},
	}
	h := NewChatHandler(db.NewRepo(database))
	first := httptest.NewRecorder()
	body := []byte(`{"model":"free-best","messages":[{"role":"user","content":"Write Go addition"}]}`)
	h.handleComboFallback(context.Background(), first, body, []string{"llamacpp/exhausted-wire", "llamacpp/qwen-wire"}, "fallback", false, false, "free-best", 0, true)
	if first.Code != 200 || exhaustedHits.Load() != 1 || healthyHits.Load() != 1 {
		t.Fatalf("fallback: %d exhausted=%d healthy=%d %s", first.Code, exhaustedHits.Load(), healthyHits.Load(), first.Body.String())
	}
	key := availability.Key{Provider: "llamacpp", Model: "exhausted-catalog", Account: routing.VirtualNoAuthAccountID("llamacpp")}
	if globalAvailability.Available(key) {
		t.Fatal("429 did not block catalog identity")
	}
	second := httptest.NewRecorder()
	h.HandleChatCompletions(second, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body))))
	if second.Code != 200 || exhaustedHits.Load() != 1 || healthyHits.Load() != 2 {
		t.Fatalf("next request: %d exhausted=%d healthy=%d %s", second.Code, exhaustedHits.Load(), healthyHits.Load(), second.Body.String())
	}
}

func TestFreeTrafficOnlyObservesFreeCatalogAndMeaningfulOutcomes(t *testing.T) {
	isolateFreeHealth(t)
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	registry.GetActiveState().ProviderModels["groq"] = map[string]*registry.ProviderModel{
		"free-catalog": {ProviderID: "groq", ModelID: "free-catalog", UpstreamModel: "wire", PricingMode: "free_tier", IsActive: true},
		"paid-catalog": {ProviderID: "groq", ModelID: "paid-catalog", UpstreamModel: "wire", PricingMode: "paid", IsActive: true},
	}
	ctx := withFreeProfile(context.Background(), "free-best", true)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"bad-request", ctx, &upstreamError{StatusCode: 400}},
		{"ordinary", context.Background(), &upstreamError{StatusCode: 429}},
		{"cancel", ctx, context.Canceled},
		{"deadline", ctx, context.DeadlineExceeded},
	} {
		recordVirtualFreeTraffic(tc.ctx, "groq", "wire", tc.name, tc.err, 10, 4)
		if v := globalAvailability.Get(availability.Key{Provider: "groq", Model: "free-catalog", Account: tc.name}); v.Attempts != 0 {
			t.Fatalf("%s poisoned health: %+v", tc.name, v)
		}
	}
	recordVirtualFreeTraffic(ctx, "groq", "wire", "actual", &upstreamError{StatusCode: 429}, 10, 4)
	if globalAvailability.Available(availability.Key{Provider: "groq", Model: "free-catalog", Account: "actual"}) {
		t.Fatal("429 ignored")
	}
	if v := globalAvailability.Get(availability.Key{Provider: "groq", Model: "paid-catalog", Account: "actual"}); v.Attempts != 0 {
		t.Fatal("paid catalog polluted")
	}
	if v := globalAvailability.Get(availability.Key{Provider: "groq", Model: "wire", Account: "actual"}); v.Attempts != 0 {
		t.Fatal("invented wire identity")
	}
	recordVirtualFreeTraffic(ctx, "groq", "wire", "network", errors.New("connection refused"), 10, 0)
	if globalAvailability.Available(availability.Key{Provider: "groq", Model: "free-catalog", Account: "network"}) {
		t.Fatal("network failure ignored")
	}
}

func TestDeclaredCapabilityMismatchStaysOutOfProfile(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)
	state := registry.GetActiveState()
	state.ProviderModels["groq"]["llama-3.1-8b:free"].Capabilities = `{"tools":false,"reasoning":false,"context_window":4096}`
	h := NewChatHandler(db.NewRepo(database))
	for _, name := range []string{"coding-best-free", "reasoning-free", "long-context-free"} {
		info, err := h.resolveModel(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, entry := range info.ComboModels {
			if entry == "groq/llama-3.1-8b-instant" {
				t.Fatalf("%s kept a model whose declared capabilities contradict the profile: %#v", name, info.ComboModels)
			}
		}
	}
}

func TestOpenRouterFreeIdentityEndToEnd(t *testing.T) {
	isolateFreeHealth(t)
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	const wire = "qwen/coder:free"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["model"] != wire {
			t.Errorf("full identity lost: %v", request["model"])
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-openrouter-key" {
			t.Error("incorrect OpenRouter authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"synthetic","choices":[{"message":{"role":"assistant","content":"synthetic code"}}]}`))
	}))
	defer srv.Close()
	original := providers.KnownProviders["openrouter"]
	configured := original
	configured.BaseURL = srv.URL
	providers.KnownProviders["openrouter"] = configured
	t.Cleanup(func() { providers.KnownProviders["openrouter"] = original })
	_, err := database.Exec(`INSERT INTO providerConnections (id,provider,authType,name,priority,isActive,data,createdAt,updatedAt) VALUES ('or-test','openrouter','apikey','protocol fixture',1,1,'{"apiKey":"synthetic-openrouter-key"}','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	state := registry.GetActiveState()
	state.Providers["openrouter"] = &registry.Provider{ID: "openrouter", IsActive: true}
	state.ProviderModels["openrouter"] = map[string]*registry.ProviderModel{
		wire: {ProviderID: "openrouter", ModelID: wire, UpstreamModel: wire, PricingMode: "free_tier", IsActive: true, Capabilities: `{"tools":true}`},
	}
	handler := NewChatHandler(db.NewRepo(database))
	out := httptest.NewRecorder()
	handler.HandleChatCompletions(out, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"coding-best-free","messages":[{"role":"user","content":"Write a function"}]}`)))
	if out.Code != 200 || hits.Load() != 1 || !strings.Contains(out.Body.String(), "synthetic code") {
		t.Fatalf("status=%d hits=%d body=%s", out.Code, hits.Load(), out.Body.String())
	}
	if observed := globalAvailability.Get(availability.Key{Provider: "openrouter", Model: wire, Account: "or-test"}); observed.Successes != 1 {
		t.Fatalf("missing identity observation: %+v", observed)
	}
}
