package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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


func TestHandleComboFallback_Opencode503SwitchesConfiguredAccount(t *testing.T) {
	const model = "audit-opencode-account-failover"

	var failedHits atomic.Int32
	failedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failedHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Endpoint is unavailable"}}`))
	}))
	defer failedUpstream.Close()

	var healthyHits atomic.Int32
	healthyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"recovered","choices":[{"message":{"content":"fallback account worked"}}]}`))
	}))
	defer healthyUpstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil {
		t.Fatalf("clear opencode connections: %v", err)
	}
	seedConnDB(t, database, "opencode", "conn-opencode-failed", "", failedUpstream.URL)
	seedConnDB(t, database, "opencode", "conn-opencode-healthy", "", healthyUpstream.URL)
	if _, err := database.Exec(`
		UPDATE providerConnections
		SET priority = CASE id
			WHEN 'conn-opencode-failed' THEN 1
			WHEN 'conn-opencode-healthy' THEN 2
		END
		WHERE id IN ('conn-opencode-failed', 'conn-opencode-healthy')
	`); err != nil {
		t.Fatalf("set deterministic priorities: %v", err)
	}

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

	if rec.Code != http.StatusOK {
		t.Fatalf("expected fallback account to succeed with 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "fallback account worked") {
		t.Fatalf("expected response from healthy account, got %s", rec.Body.String())
	}
	if got := failedHits.Load(); got != 1 {
		t.Fatalf("expected failed account once, got %d attempts", got)
	}
	if got := healthyHits.Load(); got != 1 {
		t.Fatalf("expected healthy account once, got %d attempts", got)
	}

	locked, err := repo.IsConnectionModelLocked("conn-opencode-failed", model)
	if err != nil {
		t.Fatalf("IsConnectionModelLocked: %v", err)
	}
	if !locked {
		t.Fatal("expected failed account/model to be locked after retryable 503")
	}
}

func TestHandleComboFallback_Opencode503SwitchesModelProvider(t *testing.T) {
	const failedModel = "audit-opencode-model-failover"

	var failedHits atomic.Int32
	failedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failedHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Endpoint is unavailable"}}`))
	}))
	defer failedUpstream.Close()

	var healthyHits atomic.Int32
	healthyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"provider-recovered","choices":[{"message":{"content":"next provider worked"}}]}`))
	}))
	defer healthyUpstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider IN ('opencode', 'deepseek')`); err != nil {
		t.Fatalf("clear provider connections: %v", err)
	}
	seedConnDB(t, database, "opencode", "conn-opencode-provider-failed", "", failedUpstream.URL)
	seedConnDB(t, database, "deepseek", "conn-deepseek-provider-healthy", "sk-healthy", healthyUpstream.URL)

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
		[]string{"opencode/" + failedModel, "deepseek/deepseek-chat"},
		"fallback",
		false,
		false,
		"free-best",
		0,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected next provider to succeed with 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "next provider worked") {
		t.Fatalf("expected response from next provider, got %s", rec.Body.String())
	}
	if got := failedHits.Load(); got != 1 {
		t.Fatalf("expected failed OpenCode route once, got %d attempts", got)
	}
	if got := healthyHits.Load(); got != 1 {
		t.Fatalf("expected next provider once, got %d attempts", got)
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
