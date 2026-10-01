package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestHealthCheckFreeOnlyBoundedAndRedacted(t *testing.T) {
	isolateFreeHealth(t)
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	var hits atomic.Int32
	var invalid atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if invalid.Load() {
			_, _ = w.Write([]byte(`{"choices":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"synthetic-private-output"}}]}`))
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
		"qwen": {ProviderID: "llamacpp", ModelID: "qwen", UpstreamModel: "qwen", PricingMode: "free", IsActive: true},
		"paid": {ProviderID: "llamacpp", ModelID: "paid", UpstreamModel: "paid", PricingMode: "paid", IsActive: true},
	}
	h := NewChatHandler(db.NewRepo(database))
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"model":"llamacpp/qwen"}`, 200},
		{`{"model":"llamacpp/paid"}`, 503},
		{`{"model":"free-best"}`, 400},
		{`{"model":"llamacpp/qwen","prompt":"untrusted"}`, 400},
		{`{"model":"llamacpp/qwen"} {}`, 400},
	} {
		out := httptest.NewRecorder()
		h.HandleAdminHealthCheck(out, httptest.NewRequest("POST", "/api/admin/health-check", strings.NewReader(tc.body)))
		if out.Code != tc.status {
			t.Errorf("%s: status=%d %s", tc.body, out.Code, out.Body.String())
		}
		if strings.Contains(out.Body.String(), "synthetic-private-output") {
			t.Fatal("probe exposed output")
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("unintended inference calls=%d", hits.Load())
	}
	invalid.Store(true)
	for i := 0; i < 2; i++ {
		out := httptest.NewRecorder()
		h.HandleAdminHealthCheck(out, httptest.NewRequest("POST", "/api/admin/health-check", strings.NewReader(`{"model":"llamacpp/qwen"}`)))
		if out.Code != http.StatusServiceUnavailable {
			t.Fatalf("invalid or cooled-down response accepted: %d", out.Code)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("invalid response did not suppress immediate reprobe: hits=%d", hits.Load())
	}
}
