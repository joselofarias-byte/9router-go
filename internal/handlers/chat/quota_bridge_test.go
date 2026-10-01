package chat

import (
	"9router/proxy/internal/controlplane/availability"
	"9router/proxy/internal/controlplane/capacity"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuotaBridgeAccountGateAndReportedRecovery(t *testing.T) {
	isolateFreeHealth(t)
	capacity.ClearAll()
	t.Cleanup(capacity.ClearAll)
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-key-b" {
			t.Error("exhausted account was forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"fixture answer"}}]}`))
	}))
	defer srv.Close()
	original := providers.KnownProviders["openrouter"]
	configured := original
	configured.BaseURL = srv.URL
	providers.KnownProviders["openrouter"] = configured
	t.Cleanup(func() { providers.KnownProviders["openrouter"] = original })
	seedConnDB(t, database, "openrouter", "fixture-a", "fixture-key-a", srv.URL)
	seedConnDB(t, database, "openrouter", "fixture-b", "fixture-key-b", srv.URL)
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	state := registry.GetActiveState()
	state.Providers["openrouter"] = &registry.Provider{ID: "openrouter", IsActive: true}
	state.ProviderModels["openrouter"] = map[string]*registry.ProviderModel{
		"full/catalog:free": {ProviderID: "openrouter", ModelID: "full/catalog:free", UpstreamModel: "full/wire:free", PricingMode: "free_tier", IsActive: true},
	}
	capacity.Update("openrouter", "fixture-a", "full/wire:free", 0, time.Now().Add(time.Hour))
	capacity.Update("openrouter", "fixture-b", "full/wire:free", 50, time.Now().Add(time.Hour))
	h := NewChatHandler(db.NewRepo(database))
	request := func() *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		h.HandleChatCompletions(out, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"free-best","messages":[{"role":"user","content":"fixture"}]}`)))
		return out
	}
	if out := request(); out.Code != 200 || hits.Load() != 1 {
		t.Fatalf("quota fallback %d %s", out.Code, out.Body.String())
	}
	capacity.Update("openrouter", "fixture-b", "full/wire:free", 0, time.Now().Add(time.Hour))
	if out := request(); out.Code != 503 || hits.Load() != 1 || !strings.Contains(out.Body.String(), FreeRouteUnavailableCode) {
		t.Fatalf("exhausted pool did not fail closed: %d %s", out.Code, out.Body.String())
	}
	capacity.Update("openrouter", "fixture-b", "full/wire:free", 80, time.Now().Add(time.Hour))
	if out := request(); out.Code != 200 || hits.Load() != 2 {
		t.Fatalf("recovery %d %s", out.Code, out.Body.String())
	}
	// A model remains in the pool through b; the deduplicated entry must not
	// allow a's stale health to be ignored at account selection time.
	capacity.Update("openrouter", "fixture-a", "full/wire:free", 99, time.Now().Add(time.Hour))
	globalAvailability.Observe(availability.Key{Provider: "openrouter", Model: "full/catalog:free", Account: "fixture-a"}, false, "quota", time.Minute, 0, 0)
	if out := request(); out.Code != 200 || hits.Load() != 3 {
		t.Fatalf("account health ignored: %d %s", out.Code, out.Body.String())
	}
}

func TestQuotaBridge429KeepsBalanceAndBlocksExplicitSharedScope(t *testing.T) {
	isolateFreeHealth(t)
	capacity.ClearAll()
	t.Cleanup(capacity.ClearAll)
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatal(err)
	}
	registry.GetActiveState().ProviderModels["openrouter"] = map[string]*registry.ProviderModel{
		"catalog": {ProviderID: "openrouter", ModelID: "catalog", UpstreamModel: "wire", PricingMode: "free_tier", IsActive: true},
	}
	scope := capacity.Scope{Organization: "fixture-org"}
	capacity.SetScope("openrouter", "a", scope)
	capacity.SetScope("openrouter", "b", scope)
	capacity.Update("openrouter", "a", "wire", 75, time.Now().Add(time.Hour))
	until := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	ctx := withFreeProfile(context.Background(), "free-best", true)
	recordVirtualFreeTraffic(ctx, "openrouter", "wire", "a", &upstreamError{StatusCode: 429, Body: []byte(`{"retryAfter":"` + until + `"}`)}, 0, 0)
	for _, id := range []string{"a", "b"} {
		s := capacity.Inspect("openrouter", id, "wire")
		if s.Status != capacity.Available || s.RemainingPercentage != 75 || s.BlockedUntil.Before(time.Now().Add(4*time.Minute)) {
			t.Fatalf("observed retry was official quota or scope bypassed: %+v", s)
		}
	}
}

func TestQuotaBridgeAntigravityPublisherUsesOnlyReportedQuota(t *testing.T) {
	ClearAntigravityQuotaCache()
	t.Cleanup(ClearAntigravityQuotaCache)
	const account = "fixture-account"
	const model = "gemini-3.7-flash-high"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{"` + model + `":{"quotaInfo":{"remainingFraction":0.8}},"missing":{"quotaInfo":{"resetTime":"2030-01-01T00:00:00Z"}}}}`))
	}))
	defer srv.Close()
	old := antigravityQuotaBaseURL
	antigravityQuotaBaseURL = srv.URL
	t.Cleanup(func() { antigravityQuotaBaseURL = old })
	agStrikeMu.Lock()
	agStrikeBlocks[account+"|"+model] = time.Now().Add(time.Minute)
	agStrikeMu.Unlock()
	t.Cleanup(func() { ClearAntigravityStrikes(account, model) })
	quotas, err := RefreshAntigravityQuota(context.Background(), srv.Client(), account, "fixture-token", "fixture-project")
	if err != nil {
		t.Fatal(err)
	}
	if quotas[model].RemainingPercentage != 0 {
		t.Fatal("legacy strike protection lost")
	}
	if q, ok := capacity.Get("antigravity", account, model); !ok || q.RemainingPercentage != 80 {
		t.Fatalf("strike presented as provider balance: %+v %v", q, ok)
	}
	if s := capacity.Inspect("antigravity", account, "missing"); s.Status != capacity.Unknown {
		t.Fatal("missing fraction became exhausted quota")
	}
}
