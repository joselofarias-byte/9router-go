package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestVirtualFreeLiveQuotaSkipsNoAuthProviderOnNextRequest(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	resetFabricState(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit","code":429}}`))
	}))
	defer srv.Close()

	original := providers.KnownProviders["llamacpp"]
	local := original
	local.BaseURL = srv.URL
	providers.KnownProviders["llamacpp"] = local
	defer func() { providers.KnownProviders["llamacpp"] = original }()

	state := registry.GetActiveState()
	state.Providers["llamacpp"] = &registry.Provider{ID: "llamacpp", IsActive: true}
	state.ProviderModels["llamacpp"] = map[string]*registry.ProviderModel{
		"qwen-local": {ProviderID: "llamacpp", ModelID: "qwen-local", PricingMode: "free", IsActive: true},
	}
	h := NewChatHandler(db.NewRepo(database))
	request := func() *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
			`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`))
		h.HandleChatCompletions(rec, req)
		return rec
	}

	first := request()
	if first.Code != http.StatusTooManyRequests || hits.Load() != 1 {
		t.Fatalf("first request: status=%d hits=%d body=%s", first.Code, hits.Load(), first.Body.String())
	}
	account := routing.VirtualNoAuthAccountID("llamacpp")
	if blocked, reason := globalTrustManager.IsUnavailable("llamacpp", "qwen-local", account); !blocked || reason != "quota_exhausted" {
		t.Fatalf("trust state: blocked=%v reason=%s", blocked, reason)
	}
	second := request()
	assertFreeRouteUnavailable(t, second)
	if hits.Load() != 1 {
		t.Fatalf("exhausted provider was called again: %d hits", hits.Load())
	}
}

func TestVirtualFreeTrafficMapsWireModelAndIgnoresRequestError(t *testing.T) {
	resetFabricState(t)
	state := registry.GetActiveState()
	state.ProviderModels["groq"] = map[string]*registry.ProviderModel{
		"catalog-free": {
			ProviderID: "groq", ModelID: "catalog-free", UpstreamModel: "actual-wire-model",
			PricingMode: "free", IsActive: true,
		},
		"catalog-paid": {
			ProviderID: "groq", ModelID: "catalog-paid", UpstreamModel: "actual-wire-model",
			PricingMode: "paid", IsActive: true,
		},
	}
	ctx := withVirtualFree(context.Background(), true)
	recordVirtualFreeTraffic(ctx, "groq", "actual-wire-model", "conn-1",
		&upstreamError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":{"message":"rate limit"}}`)}, 4)
	if blocked, _ := globalTrustManager.IsUnavailable("groq", "catalog-free", "conn-1"); !blocked {
		t.Fatal("live 429 did not block the catalog model")
	}
	if blocked, _ := globalTrustManager.IsUnavailable("groq", "actual-wire-model", "conn-1"); blocked {
		t.Fatal("created trust state under the wire model")
	}
	if blocked, _ := globalTrustManager.IsUnavailable("groq", "catalog-paid", "conn-1"); blocked {
		t.Fatal("free traffic altered a paid catalog entry sharing the wire model")
	}
	recordVirtualFreeTraffic(ctx, "groq", "actual-wire-model", "conn-2",
		&upstreamError{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"bad request"}}`)}, 4)
	if blocked, _ := globalTrustManager.IsUnavailable("groq", "catalog-free", "conn-2"); blocked {
		t.Fatal("request error blocked a healthy connection")
	}
	recordVirtualFreeTraffic(context.Background(), "groq", "actual-wire-model", "conn-3",
		&upstreamError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":{"message":"rate limit"}}`)}, 4)
	if blocked, _ := globalTrustManager.IsUnavailable("groq", "catalog-free", "conn-3"); blocked {
		t.Fatal("ordinary non-pool traffic changed the free pool trust state")
	}
}
