package chat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestFusionPanel_DefaultKeyProviderUsesConfiguredAccountFeedback(t *testing.T) {
	const model = "audit-fusion-model"
	const connID = "conn-opencode-fusion"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"fusion route denied","type":"FreeTierError"}}`))
	}))
	defer upstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil {
		t.Fatalf("clear opencode connections: %v", err)
	}
	seedConnDB(t, database, "opencode", connID, "", upstream.URL)

	orig := providers.KnownProviders["opencode"]
	cfg := orig
	cfg.BaseURL = upstream.URL
	providers.KnownProviders["opencode"] = cfg
	defer func() { providers.KnownProviders["opencode"] = orig }()

	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)
	call := h.makePanelCall(
		[]byte(`{"model":"ignored","messages":[{"role":"user","content":"hi"}],"stream":false}`),
		"opencode/"+model,
	)
	result := call()
	if result == nil || result.err == nil {
		t.Fatalf("expected fusion panel upstream error, got %#v", result)
	}

	if level := globalTrustManager.GetTrustLevel("opencode", model, connID); level != trust.TrustQuarantined {
		t.Fatalf("expected concrete fusion account quarantined, got %s", level)
	}
	locked, err := repo.IsConnectionModelLocked(connID, model)
	if err != nil {
		t.Fatalf("IsConnectionModelLocked: %v", err)
	}
	if !locked {
		t.Fatal("expected concrete fusion account/model lock after retryable 403")
	}
}
