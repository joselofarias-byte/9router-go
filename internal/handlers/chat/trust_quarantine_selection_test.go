package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func setupQuarantinedSelectionTest(t *testing.T) (*ChatHandler, string, string, func()) {
	t.Helper()
	const provider = "deepseek"
	const model = "audit-quarantine-model"
	const connID = "conn-trust-quarantined"

	database, cleanupDB := setupChatTestDB(t)
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = ?`, provider); err != nil {
		cleanupDB()
		t.Fatalf("clear provider connections: %v", err)
	}
	seedConnDB(t, database, provider, connID, "sk-audit", "http://127.0.0.1:1")

	if err := registry.InitRegistry(nil); err != nil {
		cleanupDB()
		t.Fatalf("init registry: %v", err)
	}
	state := registry.GetActiveState()
	state.Providers[provider] = &registry.Provider{ID: provider, IsActive: true}
	state.Models[model] = &registry.Model{ID: model, Name: model}
	state.ProviderModels[provider] = map[string]*registry.ProviderModel{
		model: {
			ProviderID: provider,
			ModelID: model,
			UpstreamModel: model,
			PricingMode: "paid",
			IsActive: true,
		},
	}

	oldTM := globalTrustManager
	oldEngine := globalRoutingEngine
	tm := trust.NewManager()
	globalTrustManager = tm
	globalRoutingEngine = &routing.Engine{TrustManager: tm}
	tm.RecordObservation(provider, model, connID, false, providers.ErrAuth)

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)
	cleanup := func() {
		globalTrustManager = oldTM
		globalRoutingEngine = oldEngine
		cleanupDB()
	}
	return h, model, connID, cleanup
}

func TestGetBestConnection_DoesNotLegacyFallbackIntoTrustQuarantine(t *testing.T) {
	h, model, _, cleanup := setupQuarantinedSelectionTest(t)
	defer cleanup()

	if _, _, err := h.getBestConnection("deepseek", "", nil, model); err == nil {
		t.Fatal("expected quarantined account to remain unavailable after Control Plane returns no candidates")
	}
}

func TestGetBestConnection_PinnedIDDoesNotBypassTrustQuarantine(t *testing.T) {
	h, model, connID, cleanup := setupQuarantinedSelectionTest(t)
	defer cleanup()

	if _, _, err := h.getBestConnection("deepseek", connID, nil, model); err == nil {
		t.Fatal("expected pinned connection to respect trust quarantine")
	}
}


func TestAccountTrustSelectable_AllowsDegradedState(t *testing.T) {
	oldTM := globalTrustManager
	tm := trust.NewManager()
	globalTrustManager = tm
	defer func() { globalTrustManager = oldTM }()

	const provider = "deepseek"
	const model = "audit-degraded-model"
	const account = "degraded-account"

	// One success establishes a verified record; a transient failure degrades it
	// without quarantining it.
	tm.RecordObservation(provider, model, account, true, "")
	tm.RecordObservation(provider, model, account, false, providers.ErrTransient)

	if level := tm.GetTrustLevel(provider, model, account); level != trust.TrustDegraded {
		t.Fatalf("expected degraded trust, got %s", level)
	}
	if !isAccountTrustSelectable(provider, model, account) {
		t.Fatal("degraded account should remain selectable")
	}
}


func TestHandleAccountFallback_QuarantinedSyntheticDefaultStopsBeforeNetwork(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"free tier denied","type":"FreeTierError"}}`))
	}))
	defer upstream.Close()

	database, cleanupDB := setupChatTestDB(t)
	defer cleanupDB()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil {
		t.Fatalf("clear opencode connections: %v", err)
	}

	orig := providers.KnownProviders["opencode"]
	cfg := orig
	cfg.BaseURL = upstream.URL
	providers.KnownProviders["opencode"] = cfg
	defer func() { providers.KnownProviders["opencode"] = orig }()

	oldTM := globalTrustManager
	oldEngine := globalRoutingEngine
	tm := trust.NewManager()
	globalTrustManager = tm
	globalRoutingEngine = &routing.Engine{TrustManager: tm}
	defer func() {
		globalTrustManager = oldTM
		globalRoutingEngine = oldEngine
	}()

	h := NewChatHandler(db.NewRepo(database))
	const model = "audit-public-default"
	body := []byte(`{"model":"audit-public-default","messages":[{"role":"user","content":"hi"}]}`)

	first := httptest.NewRecorder()
	if err := h.handleAccountFallback(context.Background(), first, "opencode", model, "", body, false, false, "/v1/chat/completions"); err == nil {
		t.Fatal("expected first public route call to return upstream 403")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected one upstream hit, got %d", got)
	}
	if level := tm.GetTrustLevel("opencode", model, "default"); level != trust.TrustQuarantined {
		t.Fatalf("expected synthetic default route quarantined, got %s", level)
	}

	second := httptest.NewRecorder()
	if err := h.handleAccountFallback(context.Background(), second, "opencode", model, "", body, false, false, "/v1/chat/completions"); err == nil {
		t.Fatal("expected quarantined public route to fail locally")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("quarantined public route retried upstream; hits=%d", got)
	}
}

func TestComboConnection_QuarantinedSyntheticDefaultIsUnavailable(t *testing.T) {
	database, cleanupDB := setupChatTestDB(t)
	defer cleanupDB()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil {
		t.Fatalf("clear opencode connections: %v", err)
	}

	oldTM := globalTrustManager
	tm := trust.NewManager()
	globalTrustManager = tm
	defer func() { globalTrustManager = oldTM }()

	const model = "audit-combo-default-quarantine"
	tm.RecordObservation("opencode", model, "default", false, providers.ErrAuth)

	h := NewChatHandler(db.NewRepo(database))
	if _, _, _, err := h.comboConnection(&ModelInfo{Provider: "opencode", Model: model}, nil); err == nil {
		t.Fatal("expected quarantined synthetic default route to be unavailable")
	}
}

func TestGetBestConnection_QuarantinedSyntheticNoAuthIsUnavailable(t *testing.T) {
	database, cleanupDB := setupChatTestDB(t)
	defer cleanupDB()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'mimo-free'`); err != nil {
		t.Fatalf("clear mimo-free connections: %v", err)
	}
	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}

	oldTM := globalTrustManager
	oldEngine := globalRoutingEngine
	tm := trust.NewManager()
	globalTrustManager = tm
	globalRoutingEngine = &routing.Engine{TrustManager: tm}
	defer func() {
		globalTrustManager = oldTM
		globalRoutingEngine = oldEngine
	}()

	const model = "audit-noauth-quarantine"
	tm.RecordObservation("mimo-free", model, "noauth", false, providers.ErrAuth)

	h := NewChatHandler(db.NewRepo(database))
	if _, _, err := h.getBestConnection("mimo-free", "", nil, model); err == nil {
		t.Fatal("expected quarantined synthetic noauth route to be unavailable")
	}
}
