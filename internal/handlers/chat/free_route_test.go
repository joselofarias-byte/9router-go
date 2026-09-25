package chat

import (
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/db"
)

func seedFreeRouteRegistry(t *testing.T) {
	t.Helper()
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}
	state := registry.GetActiveState()
	state.Providers["groq"] = &registry.Provider{ID: "groq", IsActive: true}
	state.Providers["deepseek"] = &registry.Provider{ID: "deepseek", IsActive: true}
	state.Providers["cline"] = &registry.Provider{ID: "cline", IsActive: true}
	state.ProviderModels["groq"] = map[string]*registry.ProviderModel{
		"llama-3.1-8b:free": {
			ProviderID:    "groq",
			ModelID:       "llama-3.1-8b:free",
			UpstreamModel: "llama-3.1-8b-instant",
			PricingMode:   "free",
			IsActive:      true,
		},
		"not-classified": {
			ProviderID:  "groq",
			ModelID:     "not-classified",
			PricingMode: "unknown",
			IsActive:    true,
		},
	}
	state.ProviderModels["deepseek"] = map[string]*registry.ProviderModel{
		"deepseek-chat": {
			ProviderID:  "deepseek",
			ModelID:     "deepseek-chat",
			PricingMode: "paid",
			IsActive:    true,
		},
		"deepseek-reasoner-free": {
			ProviderID:  "deepseek",
			ModelID:     "deepseek-reasoner-free",
			PricingMode: "free_tier",
			IsActive:    true,
		},
	}
	state.ProviderModels["cline"] = map[string]*registry.ProviderModel{
		"cline-free-model": {
			ProviderID:  "cline",
			ModelID:     "cline-free-model",
			PricingMode: "free",
			IsActive:    true,
		},
	}
}

func TestResolveModel_FreeBestUsesDiscoveredFreeModels(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	// Inactive local connection must not make the Cline free model routable.
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-cline', 'cline', 'apikey', 'Cline off', 1, 0, '{}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`); err != nil {
		t.Fatalf("seed inactive cline connection: %v", err)
	}

	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))

	for _, name := range []string{"free-best", "FREE", "Free-Best"} {
		info, err := h.resolveModel(name)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		if info.Strategy != "fallback" {
			t.Fatalf("strategy for %s = %q", name, info.Strategy)
		}
		got := map[string]bool{}
		for _, entry := range info.ComboModels {
			got[entry] = true
		}
		for _, want := range []string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"} {
			if !got[want] {
				t.Errorf("%s missing %s in %#v", name, want, got)
			}
		}
		for _, banned := range []string{"deepseek/deepseek-chat", "groq/not-classified", "cline/cline-free-model", "groq/llama-3.1-8b:free"} {
			if got[banned] {
				t.Errorf("%s included %s", name, banned)
			}
		}
		if len(got) != 2 {
			t.Fatalf("%s expected 2 free models, got %#v", name, got)
		}
	}
}

func TestResolveModel_ExplicitFreeComboWins(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)

	models, _ := json.Marshal([]string{"deepseek/deepseek-chat"})
	if _, err := database.Exec(`INSERT INTO combos (id, name, kind, models, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?)`,
		"user-free", "free", "fallback", string(models), "2026-07-19T00:00:00Z", "2026-07-19T00:00:00Z"); err != nil {
		t.Fatalf("seed combo: %v", err)
	}

	h := NewChatHandler(db.NewRepo(database))
	info, err := h.resolveModel("free")
	if err != nil {
		t.Fatalf("resolve explicit combo: %v", err)
	}
	if len(info.ComboModels) != 1 || info.ComboModels[0] != "deepseek/deepseek-chat" {
		t.Fatalf("explicit combo was replaced by the dynamic pool: %#v", info.ComboModels)
	}
}

func TestResolveModel_FreeBestDoesNotFallThroughToPaidProvider(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}

	h := NewChatHandler(db.NewRepo(database))
	info, err := h.resolveModel("free-best")
	if err == nil {
		t.Fatalf("expected error when no free models are discovered, got %+v", info)
	}
	if info != nil && info.Provider == "deepseek" {
		t.Fatalf("free-best fell through to paid provider %+v", info)
	}
}

func TestHandleModels_ListsVirtualFreeRoutes(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := map[string]string{}
	for _, m := range resp.Data {
		found[m.ID] = m.OwnedBy
	}
	for _, id := range []string{"free", "free-best"} {
		if found[id] != "fabric" {
			t.Errorf("model %s owned_by=%q, want fabric", id, found[id])
		}
	}
}

func TestResolveModel_PaidAndUnclassifiedPoolFailsClosed(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}
	state := registry.GetActiveState()
	state.Providers["groq"] = &registry.Provider{ID: "groq", IsActive: true}
	state.Providers["deepseek"] = &registry.Provider{ID: "deepseek", IsActive: true}
	state.ProviderModels["groq"] = map[string]*registry.ProviderModel{
		"not-classified": {ProviderID: "groq", ModelID: "not-classified", PricingMode: "unknown", IsActive: true},
		"trial":          {ProviderID: "groq", ModelID: "trial", PricingMode: "trial", IsActive: true},
		"upper":          {ProviderID: "groq", ModelID: "upper", PricingMode: "FREE", IsActive: true},
	}
	state.ProviderModels["deepseek"] = map[string]*registry.ProviderModel{
		"deepseek-chat": {ProviderID: "deepseek", ModelID: "deepseek-chat", PricingMode: "paid", IsActive: true},
	}

	h := NewChatHandler(db.NewRepo(database))
	for _, name := range []string{"free", "free-best", "FREE", " Free-Best "} {
		info, err := h.resolveModel(name)
		if err == nil || info != nil {
			t.Fatalf("%s resolved to %+v, want fail-closed error", name, info)
		}
	}
}

func TestResolveModel_DisconnectAndReconnectChangesFreePool(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}
	state := registry.GetActiveState()
	state.Providers["groq"] = &registry.Provider{ID: "groq", IsActive: true}
	state.Providers["deepseek"] = &registry.Provider{ID: "deepseek", IsActive: true}
	state.Providers["ghost"] = &registry.Provider{ID: "ghost", IsActive: false}
	state.ProviderModels["groq"] = map[string]*registry.ProviderModel{
		"llama": {ProviderID: "groq", ModelID: "llama", UpstreamModel: "llama-free", PricingMode: "free", IsActive: true},
		"off":   {ProviderID: "groq", ModelID: "off", UpstreamModel: "llama-off", PricingMode: "free", IsActive: false},
	}
	state.ProviderModels["deepseek"] = map[string]*registry.ProviderModel{
		"deepseek-chat": {ProviderID: "deepseek", ModelID: "deepseek-chat", PricingMode: "paid", IsActive: true},
	}
	state.ProviderModels["ghost"] = map[string]*registry.ProviderModel{
		"ghost-free": {ProviderID: "ghost", ModelID: "ghost-free", PricingMode: "free", IsActive: true},
	}
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-ghost', 'ghost', 'apikey', 'Ghost', 1, 1, '{}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`); err != nil {
		t.Fatalf("seed ghost connection: %v", err)
	}

	h := NewChatHandler(db.NewRepo(database))
	info, err := h.resolveModel("free-best")
	if err != nil {
		t.Fatalf("connected free route: %v", err)
	}
	assertExactPool(t, info, []string{"groq/llama-free"})

	if _, err := database.Exec(`UPDATE providerConnections SET isActive = 0 WHERE id = 'conn-2'`); err != nil {
		t.Fatalf("deactivate groq: %v", err)
	}
	info, err = h.resolveModel("FREE")
	if err == nil || info != nil {
		t.Fatalf("disconnected free route resolved to %+v", info)
	}

	if _, err := database.Exec(`DELETE FROM providerConnections WHERE id = 'conn-2'`); err != nil {
		t.Fatalf("delete groq: %v", err)
	}
	info, err = h.resolveModel("free")
	if err == nil || info != nil {
		t.Fatalf("deleted provider resolved to %+v", info)
	}

	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-2b', 'groq', 'apikey', 'Groq again', 1, 1, '{}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`); err != nil {
		t.Fatalf("reconnect groq: %v", err)
	}
	info, err = h.resolveModel(" free-best ")
	if err != nil {
		t.Fatalf("reconnected free route: %v", err)
	}
	assertExactPool(t, info, []string{"groq/llama-free"})
}

func TestResolveModel_ExplicitFreeOverrideWinsAnyCase(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)

	models, _ := json.Marshal([]string{"deepseek/deepseek-chat"})
	if _, err := database.Exec(`INSERT INTO combos (id, name, kind, models, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?)`,
		"user-free", "free", "fallback", string(models), "2026-07-19T00:00:00Z", "2026-07-19T00:00:00Z"); err != nil {
		t.Fatalf("seed combo: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO kv (scope, key, value) VALUES ('modelAliases', 'Free-Best', '"groq/llama-3.1-8b-instant"')`); err != nil {
		t.Fatalf("seed alias: %v", err)
	}

	h := NewChatHandler(db.NewRepo(database))
	for _, name := range []string{"FREE", " free "} {
		info, err := h.resolveModel(name)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		if len(info.ComboModels) != 1 || info.ComboModels[0] != "deepseek/deepseek-chat" {
			t.Fatalf("%s did not keep the explicit combo: %#v", name, info.ComboModels)
		}
	}
	for _, name := range []string{"free-best", "FREE-BEST"} {
		info, err := h.resolveModel(name)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		if info.Provider != "groq" || info.Model != "llama-3.1-8b-instant" || len(info.ComboModels) != 0 {
			t.Fatalf("%s did not keep the explicit alias: %+v", name, info)
		}
	}
}

func TestResolveModel_VirtualFreeDoesNotChangeNormalRouting(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))

	info, err := h.resolveModel("deepseek/deepseek-chat")
	if err != nil || info.Provider != "deepseek" || info.Model != "deepseek-chat" || len(info.ComboModels) != 0 {
		t.Fatalf("provider/model route changed: %+v %v", info, err)
	}
	info, err = h.resolveModel("fast-model")
	if err != nil || info.Provider != "deepseek" || info.Model != "deepseek-chat" {
		t.Fatalf("alias route changed: %+v %v", info, err)
	}
	info, err = h.resolveModel("my-combo")
	if err != nil || info.Provider != "deepseek" || info.Model != "deepseek-chat" || len(info.ComboModels) != 1 {
		t.Fatalf("combo route changed: %+v %v", info, err)
	}
	info, err = h.resolveModel("free-tier")
	if err != nil || info.Provider != "deepseek" || info.Model != "free-tier" || len(info.ComboModels) != 0 {
		t.Fatalf("bare free-tier should stay on the common fallback: %+v %v", info, err)
	}
	info, err = h.resolveModel("openai/free")
	if err != nil || info.Provider != "openai" || info.Model != "free" || len(info.ComboModels) != 0 {
		t.Fatalf("explicit openai/free was rewritten: %+v %v", info, err)
	}
	info, err = h.resolveModel("openai/free-best")
	if err != nil || info.Provider != "openai" || info.Model != "free-best" {
		t.Fatalf("explicit openai/free-best was rewritten: %+v %v", info, err)
	}
}

func TestHandleModels_ExplicitFreeNameKeepsVirtualSibling(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	models, _ := json.Marshal([]string{"deepseek/deepseek-chat"})
	if _, err := database.Exec(`INSERT INTO combos (id, name, kind, models, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?)`,
		"user-free", "Free", "fallback", string(models), "2026-07-19T00:00:00Z", "2026-07-19T00:00:00Z"); err != nil {
		t.Fatalf("seed combo: %v", err)
	}

	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := map[string]string{}
	for _, m := range resp.Data {
		found[m.ID] = m.OwnedBy
	}
	if found["Free"] != "system" {
		t.Fatalf("explicit Free combo missing: %#v", found)
	}
	if _, dup := found["free"]; dup {
		t.Fatal("virtual free was listed beside the explicit Free combo")
	}
	if found["free-best"] != "fabric" {
		t.Fatalf("free-best owned_by=%q, want fabric", found["free-best"])
	}
}

func TestResolveModel_FreeBestConcurrentExcludesPaid(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))

	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := h.resolveModel("free-best")
			if err != nil {
				errCh <- err
				return
			}
			for _, entry := range info.ComboModels {
				switch entry {
				case "groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free":
				default:
					errCh <- fmt.Errorf("unexpected candidate %s", entry)
				}
			}
			if len(info.ComboModels) != 2 {
				errCh <- fmt.Errorf("candidate count %d", len(info.ComboModels))
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestFreeRouteEntries_RejectsPaidInactiveAndDisconnected(t *testing.T) {
	state := &registry.RegistryState{
		Providers: map[string]*registry.Provider{
			"groq":     {ID: "groq", IsActive: true},
			"deepseek": {ID: "deepseek", IsActive: true},
			"ghost":    {ID: "ghost", IsActive: false},
		},
		ProviderModels: map[string]map[string]*registry.ProviderModel{
			"groq": {
				"llama": {ProviderID: "groq", ModelID: "llama", UpstreamModel: "llama-free", PricingMode: "free", IsActive: true},
			},
			"deepseek": {
				"deepseek-chat": {ProviderID: "deepseek", ModelID: "deepseek-chat", PricingMode: "paid", IsActive: true},
				"mystery":       {ProviderID: "deepseek", ModelID: "mystery", PricingMode: "unknown", IsActive: true},
			},
			"ghost": {
				"ghost-free": {ProviderID: "ghost", ModelID: "ghost-free", PricingMode: "free_tier", IsActive: true},
			},
		},
		Accounts: map[string]*registry.Account{
			"groq-on":  {ID: "groq-on", ProviderID: "groq", IsActive: true},
			"groq-off": {ID: "groq-off", ProviderID: "groq", IsActive: false},
			"ds-on":    {ID: "ds-on", ProviderID: "deepseek", IsActive: true},
			"ghost-on": {ID: "ghost-on", ProviderID: "ghost", IsActive: true},
		},
	}
	got := freeRouteEntries(state, []routing.RouteNode{
		{ProviderID: "groq", ModelID: "llama", UpstreamModel: "stale-paid-name", AccountID: "groq-off"},
		{ProviderID: "groq", ModelID: "llama", UpstreamModel: "stale-paid-name", AccountID: "groq-on"},
		{ProviderID: "deepseek", ModelID: "deepseek-chat", AccountID: "ds-on"},
		{ProviderID: "deepseek", ModelID: "mystery", AccountID: "ds-on"},
		{ProviderID: "ghost", ModelID: "ghost-free", AccountID: "ghost-on"},
	})
	if len(got) != 1 || got[0] != "groq/llama-free" {
		t.Fatalf("entries = %#v, want only the current free upstream", got)
	}
}

func assertExactPool(t *testing.T, info *ModelInfo, want []string) {
	t.Helper()
	got := map[string]bool{}
	for _, entry := range info.ComboModels {
		got[entry] = true
	}
	if len(got) != len(want) {
		t.Fatalf("pool = %#v, want %v", got, want)
	}
	for _, entry := range want {
		if !got[entry] {
			t.Fatalf("pool = %#v, missing %s", got, entry)
		}
	}
}
