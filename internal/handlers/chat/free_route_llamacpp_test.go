package chat

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	json "encoding/json/v2"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestFreeRouteEntries_AllowsVirtualNoAuthProvider(t *testing.T) {
	state := &registry.RegistryState{
		Providers: map[string]*registry.Provider{
			"llamacpp": {ID: "llamacpp", IsActive: true},
		},
		ProviderModels: map[string]map[string]*registry.ProviderModel{
			"llamacpp": {
				"qwen-local": {
					ProviderID:    "llamacpp",
					ModelID:       "qwen-local",
					UpstreamModel: "qwen-local",
					PricingMode:   "free",
					IsActive:      true,
				},
			},
		},
		Accounts: map[string]*registry.Account{},
	}

	valid := routing.RouteNode{
		ProviderID: "llamacpp",
		ModelID:    "qwen-local",
		AccountID:  routing.VirtualNoAuthAccountID("llamacpp"),
	}
	got := freeRouteEntries(state, []routing.RouteNode{valid})
	if len(got) != 1 || got[0] != "llamacpp/qwen-local" {
		t.Fatalf("virtual no-auth route = %#v", got)
	}

	invalid := valid
	invalid.AccountID = "noauth:someone-else"
	if got := freeRouteEntries(state, []routing.RouteNode{invalid}); len(got) != 0 {
		t.Fatalf("forged virtual no-auth identity was accepted: %#v", got)
	}
}

func TestHandleChatCompletions_FreeBestRoutesToLlamaCppWithoutCredentialRow(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("llamacpp no-auth route leaked Authorization header: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode llama.cpp request: %v", err)
		}
		if req["model"] != "qwen-local" {
			t.Errorf("llama.cpp model = %v, want qwen-local", req["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"local-ok","model":"qwen-local","choices":[{"message":{"role":"assistant","content":"local"}}]}`))
	}))
	defer srv.Close()

	original := providers.KnownProviders["llamacpp"]
	local := original
	local.BaseURL = srv.URL
	providers.KnownProviders["llamacpp"] = local
	defer func() { providers.KnownProviders["llamacpp"] = original }()

	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}
	state := registry.GetActiveState()
	state.Providers["llamacpp"] = &registry.Provider{ID: "llamacpp", IsActive: true}
	state.ProviderModels["llamacpp"] = map[string]*registry.ProviderModel{
		"qwen-local": {
			ProviderID:    "llamacpp",
			ModelID:       "qwen-local",
			UpstreamModel: "qwen-local",
			PricingMode:   "free",
			IsActive:      true,
		},
	}

	h := NewChatHandler(db.NewRepo(database))
	for i, model := range []string{"free-best", "coding-auto"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
			fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hola"}]}`, model)))
		h.HandleChatCompletions(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", model, rec.Code, rec.Body.String())
		}
		if hits.Load() != int32(i+1) {
			t.Fatalf("%s llama.cpp hits = %d, want %d", model, hits.Load(), i+1)
		}
		if !strings.Contains(rec.Body.String(), `"local"`) {
			t.Fatalf("%s unexpected local response: %s", model, rec.Body.String())
		}
	}
}
