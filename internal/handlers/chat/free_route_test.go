package chat

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/controlplane/registry"
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
