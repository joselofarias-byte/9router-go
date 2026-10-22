//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"9router/proxy/internal/auth"
	"9router/proxy/internal/controlplane/registry"
	cpsync "9router/proxy/internal/controlplane/sync"
)

// TestFreeBestProductionRouterFailover proves the production router keeps a
// free-best turn on the next eligible free provider and never dials a paid or
// unknown-cost route. Every upstream is a local fake.
func TestFreeBestProductionRouterFailover(t *testing.T) {
	env := newEnv(t)
	var failHits, paidHits int
	failed := env.NewUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		failHits++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"unavailable"}}`))
	})
	ok := env.NewUpstream(t, chatCompletionResponder())
	paid := env.NewUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		paidHits++
		w.WriteHeader(http.StatusPaymentRequired)
	})
	env.AddConnection(t, "conn-groq-free", "groq", "Groq free", failed, "gsk-fake")
	env.AddConnection(t, "conn-deepseek-free", "deepseek", "DeepSeek free", ok, "sk-fake")
	env.AddConnection(t, "conn-openai-paid", "openai", "OpenAI paid", paid, "sk-paid-fake")

	restore := installFreeBestFixture(t)
	t.Cleanup(restore)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := env.Repo.RawDB().Exec(`INSERT INTO requestDetails (id, timestamp, provider, model, connectionId, status, data) VALUES (?, ?, 'groq', 'fail-free', 'conn-groq-free', 'success', '{"latency":{"total":80},"response":{"status":200}}')`, "rd-free-best-1", stamp); err != nil {
		t.Fatalf("seed health: %v", err)
	}

	res := env.Post(t, "/v1/chat/completions", ChatBody("free-best", false))
	if res.Status != http.StatusOK || !strings.Contains(string(res.Body), "upstream reply") {
		t.Fatalf("free-best status=%d body=%s", res.Status, res.Body)
	}
	if failHits != 1 || failed.Count() != 1 {
		t.Fatalf("failing free provider hits=%d recorded=%d", failHits, failed.Count())
	}
	if ok.Count() != 1 || ok.Last(t).Model(t) != "ok-free" {
		t.Fatalf("healthy free provider count=%d model=%s", ok.Count(), ok.Last(t).Model(t))
	}
	if paidHits != 0 || paid.Count() != 0 {
		t.Fatalf("paid provider was called %d times", paid.Count())
	}

	models := env.Get(t, "/v1/models")
	if !strings.Contains(string(models.Body), `"free-best"`) {
		t.Fatalf("routable free-best missing from /v1/models: %s", models.Body)
	}

	denied := env.Get(t, "/api/admin/free-best?model=free-best")
	if denied.Status != http.StatusUnauthorized {
		t.Fatalf("engine key diagnosis status=%d body=%s", denied.Status, denied.Body)
	}
	admin := env.Get(t, "/api/admin/free-best?model=free-best", WithAPIKey(""), WithHeader(auth.CLITokenHeader, auth.CLIToken()))
	if admin.Status != http.StatusOK {
		t.Fatalf("admin diagnosis status=%d body=%s", admin.Status, admin.Body)
	}
	if strings.Contains(string(admin.Body), "sk-fake") || strings.Contains(string(admin.Body), "sk-paid") || strings.Contains(string(admin.Body), "gsk-fake") || strings.Contains(string(admin.Body), "apiKey") {
		t.Fatalf("diagnosis leaked credentials: %s", admin.Body)
	}
	body := string(admin.Body)
	if !strings.Contains(body, `"reason":"paid"`) || !strings.Contains(body, `"reason":"unknown_cost"`) {
		t.Fatalf("diagnosis missing discard reasons: %s", body)
	}
	if !strings.Contains(body, `"pricing":"free_tier"`) || !strings.Contains(body, `"pricing":"free"`) {
		t.Fatalf("diagnosis hid the real pricing mode: %s", body)
	}
	if !strings.Contains(body, `"state":"gateway_temporary"`) {
		t.Fatalf("503 cooldown was not kept distinct from quota: %s", body)
	}
	if strings.Contains(body, `"quota":0`) || strings.Contains(body, `"quota":"0"`) || strings.Contains(body, `"quota":"available"`) {
		t.Fatalf("unknown quota presented as a capacity: %s", body)
	}
}

func installFreeBestFixture(t *testing.T) func() {
	t.Helper()
	if registry.GetActiveState() == nil {
		if err := registry.InitRegistry(nil); err != nil {
			t.Fatalf("init registry: %v", err)
		}
	}
	state := registry.GetActiveState()
	type saved struct {
		provider *registry.Provider
		models   map[string]*registry.ProviderModel
	}
	before := map[string]saved{}
	for _, id := range []string{"groq", "deepseek", "openai"} {
		before[id] = saved{provider: state.Providers[id], models: state.ProviderModels[id]}
	}
	state.Providers["groq"] = &registry.Provider{ID: "groq", IsActive: true}
	state.Providers["deepseek"] = &registry.Provider{ID: "deepseek", IsActive: true}
	state.Providers["openai"] = &registry.Provider{ID: "openai", IsActive: true}
	state.ProviderModels["groq"] = map[string]*registry.ProviderModel{
		"fail-free": {ProviderID: "groq", ModelID: "fail-free", UpstreamModel: "fail-free", PricingMode: "free", IsActive: true},
		"mystery":   {ProviderID: "groq", ModelID: "mystery", UpstreamModel: "mystery", PricingMode: "unknown", IsActive: true},
	}
	state.ProviderModels["deepseek"] = map[string]*registry.ProviderModel{
		"ok-free": {ProviderID: "deepseek", ModelID: "ok-free", UpstreamModel: "ok-free", PricingMode: "free_tier", IsActive: true},
	}
	state.ProviderModels["openai"] = map[string]*registry.ProviderModel{
		"gpt-4o": {ProviderID: "openai", ModelID: "gpt-4o", UpstreamModel: "gpt-4o", PricingMode: "paid", IsActive: true},
	}
	cpsync.InvalidateAccountSync()
	return func() {
		for id, snap := range before {
			if snap.provider == nil {
				delete(state.Providers, id)
			} else {
				state.Providers[id] = snap.provider
			}
			if snap.models == nil {
				delete(state.ProviderModels, id)
			} else {
				state.ProviderModels[id] = snap.models
			}
		}
		cpsync.InvalidateAccountSync()
	}
}
