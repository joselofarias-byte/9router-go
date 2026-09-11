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

// TestFabricProber_RecordsLatencyOnceViaSharedHotPath verifies a probe's
// latency is captured through the same recording added to
// tryForwardWithConnection for real traffic (fallback.go), and only once —
// Probe used to make its own separate RecordLatency call after dispatch,
// which would now double-count against the same trust-manager average real
// requests feed through that shared function.
func TestFabricProber_RecordsLatencyOnceViaSharedHotPath(t *testing.T) {
	resetFabricRoutingStateForTests()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ok","model":"deepseek-chat","choices":[{"message":{"content":"pong"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedConnDB(t, database, "deepseek", "conn-probe-latency", "sk-probe", srv.URL)

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	// Seed one known sample (1000ms) before probing so the post-probe average
	// reveals how many samples the probe itself contributed: with exactly one
	// (correct — no double-counting) the average of {1000, x} lands close to
	// 500ms for a fast local httptest round trip; with two (the bug this
	// guards against) the average of {1000, x, x} would land noticeably
	// lower, close to 333ms.
	globalTrustManager.RecordLatency("deepseek", "deepseek-chat", "conn-probe-latency", 1000)

	result, err := NewFabricProber(h).Probe(context.Background(), "deepseek", "deepseek-chat", "conn-probe-latency")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got failure: %s", result.ErrorMessage)
	}

	avgMs, hasData := globalTrustManager.LatencyStats("deepseek", "deepseek-chat", "conn-probe-latency")
	if !hasData {
		t.Fatal("expected latency samples recorded")
	}
	if avgMs < 400 {
		t.Errorf("average latency %dms is too low for 2 samples (1000ms seed + 1 probe) — probe latency was likely recorded more than once", avgMs)
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
