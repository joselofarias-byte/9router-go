package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestFabricProber_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ok","model":"deepseek-chat","choices":[{"message":{"content":"pong"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedConnDB(t, database, "deepseek", "conn-probe-ok", "sk-probe", srv.URL)

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	result, err := NewFabricProber(h).Probe(context.Background(), "deepseek", "deepseek-chat", "conn-probe-ok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got failure: %s", result.ErrorMessage)
	}
	if !result.FingerprintMatches {
		t.Error("expected fingerprint match for identical model id")
	}
	if result.LatencyMs < 0 {
		t.Errorf("expected non-negative latency, got %d", result.LatencyMs)
	}
	if !result.CapabilitiesVerified["chat"] {
		t.Error("expected chat capability to be verified on success")
	}
}

func TestFabricProber_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedConnDB(t, database, "deepseek", "conn-probe-429", "sk-probe", srv.URL)

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	result, err := NewFabricProber(h).Probe(context.Background(), "deepseek", "deepseek-chat", "conn-probe-429")
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure on 429 upstream response")
	}
	if result.ErrorCategory != providers.ErrRateLimit {
		t.Errorf("expected rate_limit classification, got %q", result.ErrorCategory)
	}
}

func TestFabricProber_UnknownAccount(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	result, err := NewFabricProber(h).Probe(context.Background(), "deepseek", "deepseek-chat", "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for unknown account")
	}
	if result.ErrorCategory != providers.ErrPermanent {
		t.Errorf("expected permanent classification, got %q", result.ErrorCategory)
	}
}
