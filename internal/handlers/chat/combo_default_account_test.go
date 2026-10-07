package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestHandleComboFallback_DefaultKeyProviderUsesConfiguredAccountFeedback(t *testing.T) {
	const model = "audit-default-account-model"
	const connID = "conn-opencode-audit"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"free route denied","type":"FreeTierError"}}`))
	}))
	defer upstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil {
		t.Fatalf("clear opencode connections: %v", err)
	}
	seedConnDB(t, database, "opencode", connID, "", upstream.URL)

	orig, ok := providers.KnownProviders["opencode"]
	if !ok {
		t.Fatal("opencode provider config missing")
	}
	cfg := orig
	cfg.BaseURL = upstream.URL
	providers.KnownProviders["opencode"] = cfg
	defer func() { providers.KnownProviders["opencode"] = orig }()

	if err := registry.InitRegistry(nil); err != nil {
		t.Fatalf("init registry: %v", err)
	}

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	body := []byte(`{"model":"free-best","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	rec := httptest.NewRecorder()
	h.handleComboFallback(
		context.Background(),
		rec,
		body,
		[]string{"opencode/" + model},
		"fallback",
		false,
		false,
		"free-best",
		0,
	)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected upstream 403, got %d: %s", rec.Code, rec.Body.String())
	}

	if level := globalTrustManager.GetTrustLevel("opencode", model, connID); level != trust.TrustQuarantined {
		t.Fatalf("expected configured account trust to be quarantined, got %s", level)
	}

	locked, err := repo.IsConnectionModelLocked(connID, model)
	if err != nil {
		t.Fatalf("IsConnectionModelLocked: %v", err)
	}
	if !locked {
		t.Fatal("expected configured account/model to be locked after retryable 403")
	}

	// Once the configured account is locked, the provider's built-in default
	// credential must not bypass that account policy.
	modelInfo := &ModelInfo{Provider: "opencode", Model: model}
	if _, _, _, err := h.comboConnection(modelInfo, nil); err == nil {
		t.Fatal("expected locked configured account to block synthetic default-key fallback")
	}
}


func TestComboConnection_DefaultKeyUsedOnlyWhenNoConfiguredAccountsExist(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil {
		t.Fatalf("clear opencode connections: %v", err)
	}

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)
	modelInfo := &ModelInfo{Provider: "opencode", Model: "audit-public-model"}

	connID, connData, oneShot, err := h.comboConnection(modelInfo, nil)
	if err != nil {
		t.Fatalf("expected built-in default route with no configured accounts: %v", err)
	}
	if connID != "default" {
		t.Fatalf("expected synthetic default connection, got %q", connID)
	}
	if connData == nil || connData.APIKey != providers.KnownProviders["opencode"].DefaultAPIKey {
		t.Fatalf("expected provider default API key, got %#v", connData)
	}
	if !oneShot {
		t.Fatal("expected synthetic default route to be one-shot")
	}
}
