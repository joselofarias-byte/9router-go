package chat

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
	"9router/proxy/internal/proxy/executor"
)

// fabricTestEnv resets the shared Fabric routing globals and the Control
// Plane registry, then returns a fresh ChatHandler backed by a real (SQLite)
// test DB. The globals are process-lifetime singletons in production, so
// every fabric-free test must start from a clean slate or one test's
// recorded trust observations would leak into another's candidate ordering.
func fabricTestEnv(t *testing.T) (*ChatHandler, *sql.DB, func()) {
	t.Helper()
	resetFabricRoutingStateForTests()
	registry.InitRegistry(nil)
	database, cleanup := setupChatTestDB(t)
	repo := db.NewRepo(database)
	return NewChatHandler(repo), database, cleanup
}

// seedFabricRoute registers a free/free-tier route in both the DB
// (providerConnections, so getBestConnection/account sync can find real
// credentials) and the in-memory Control Plane registry (so the pool
// expansion sees it), mirroring what discovery + account sync do together in
// production.
func seedFabricRoute(t *testing.T, database *sql.DB, provider, model, connID, baseURL, pricing string) {
	t.Helper()
	seedConnDB(t, database, provider, connID, "sk-"+connID, baseURL)

	state := registry.GetActiveState()
	state.Providers[provider] = &registry.Provider{ID: provider, IsActive: true}
	if state.ProviderModels[provider] == nil {
		state.ProviderModels[provider] = map[string]*registry.ProviderModel{}
	}
	state.ProviderModels[provider][model] = &registry.ProviderModel{
		ProviderID:    provider,
		ModelID:       model,
		UpstreamModel: model,
		PricingMode:   pricing,
		IsActive:      true,
	}
	state.Accounts[connID] = &registry.Account{ID: connID, ProviderID: provider, IsActive: true}
}

func jsonSuccessServer(content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"chatcmpl-x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`, content)
	}))
}

func jsonErrorServer(status int, message string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":{"message":%q}}`, message)
	}))
}

// --- resolveModel-level pool resolution ---

func TestResolveModel_FabricFree_HeterogeneousExpansion(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused-a", "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", "http://unused-b", "free_tier")

	info, err := h.ResolveModel(pools.FabricFree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Strategy != "fallback" {
		t.Errorf("expected fallback strategy for a pool, got %q", info.Strategy)
	}
	if len(info.ComboModels) != 2 {
		t.Fatalf("expected 2 heterogeneous candidates, got %d: %v", len(info.ComboModels), info.ComboModels)
	}
	seen := map[string]bool{}
	for _, e := range info.ComboModels {
		seen[e] = true
	}
	if !seen["prov-a/model-x"] || !seen["prov-b/model-y"] {
		t.Fatalf("expected both providers/models present, got %v", info.ComboModels)
	}
}

func TestResolveModel_FabricFree_NoPaidLeakage(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	seedFabricRoute(t, database, "prov-free", "model-free", "conn-free", "http://unused", "free")
	seedFabricRoute(t, database, "prov-paid", "model-paid", "conn-paid", "http://unused", "paid")

	info, err := h.ResolveModel(pools.FabricFree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range info.ComboModels {
		if strings.HasPrefix(e, "prov-paid/") {
			t.Fatalf("paid provider leaked into fabric-free pool: %v", info.ComboModels)
		}
	}
}

func TestResolveModel_FabricFree_NoEligibleCandidates(t *testing.T) {
	h, _, cleanup := fabricTestEnv(t)
	defer cleanup()

	_, err := h.ResolveModel(pools.FabricFree)
	if err == nil {
		t.Fatalf("expected an error when no free routes are registered")
	}
}

// --- direct classification / quarantine / recovery (exercises the exact
// recordRouteOutcome glue combo.go calls on every real request outcome) ---

func TestFabricFree_401Failover_Quarantines(t *testing.T) {
	_, database, cleanup := fabricTestEnv(t)
	defer cleanup()
	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused", "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", "http://unused", "free")

	recordRouteOutcome("prov-a", "model-x", "conn-a", false, http.StatusUnauthorized, []byte(`{"error":{"message":"invalid api key"}}`))

	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl != trust.TrustQuarantined {
		t.Fatalf("expected quarantined after 401, got %s", lvl)
	}
	nodes := globalRoutingEngine.SelectCandidates(pools.FabricFree, routing.PolicyFreeOnly)
	for _, n := range nodes {
		if n.ProviderID == "prov-a" {
			t.Fatalf("expected prov-a excluded from pool after 401 quarantine, got %+v", nodes)
		}
	}
	if len(nodes) != 1 || nodes[0].ProviderID != "prov-b" {
		t.Fatalf("expected only prov-b eligible, got %+v", nodes)
	}
}

func TestFabricFree_403Failover_Quarantines(t *testing.T) {
	_, database, cleanup := fabricTestEnv(t)
	defer cleanup()
	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused", "free")

	recordRouteOutcome("prov-a", "model-x", "conn-a", false, http.StatusForbidden, []byte(`{"error":{"message":"forbidden"}}`))

	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl != trust.TrustQuarantined {
		t.Fatalf("expected quarantined after 403, got %s", lvl)
	}
}

func TestFabricFree_ModelNotFound404_Quarantines(t *testing.T) {
	_, database, cleanup := fabricTestEnv(t)
	defer cleanup()
	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused", "free")

	recordRouteOutcome("prov-a", "model-x", "conn-a", false, http.StatusNotFound, []byte(`{"error":{"message":"model not found"}}`))

	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl != trust.TrustQuarantined {
		t.Fatalf("expected quarantined (permanent) after 404, got %s", lvl)
	}
}

func TestFabricFree_429_DoesNotQuarantine(t *testing.T) {
	_, database, cleanup := fabricTestEnv(t)
	defer cleanup()
	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused", "free")

	recordRouteOutcome("prov-a", "model-x", "conn-a", false, http.StatusTooManyRequests, []byte(`{"error":{"message":"rate limit exceeded"}}`))

	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl == trust.TrustQuarantined {
		t.Fatalf("a single 429 must not be treated as permanent death, got quarantined")
	}
	nodes := globalRoutingEngine.SelectCandidates(pools.FabricFree, routing.PolicyFreeOnly)
	if len(nodes) != 1 {
		t.Fatalf("expected the rate-limited route to remain eligible as a fallback, got %+v", nodes)
	}
}

func TestFabricFree_5xxAndTimeout_TransientDoesNotQuarantine(t *testing.T) {
	_, database, cleanup := fabricTestEnv(t)
	defer cleanup()
	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused", "free")

	recordRouteOutcome("prov-a", "model-x", "conn-a", false, http.StatusServiceUnavailable, []byte(`{"error":{"message":"overloaded"}}`))
	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl == trust.TrustQuarantined {
		t.Fatalf("a transient 503 must not quarantine")
	}

	// A plain transport error (timeout, connection reset) has no status code.
	recordRouteOutcome("prov-a", "model-x", "conn-a", false, 0, nil)
	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl == trust.TrustQuarantined {
		t.Fatalf("a transport-level timeout must not quarantine")
	}
}

func TestFabricFree_RecoveryAfterDegradation(t *testing.T) {
	_, database, cleanup := fabricTestEnv(t)
	defer cleanup()
	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", "http://unused", "free")

	scoreFor := func() float64 {
		for _, n := range globalRoutingEngine.SelectCandidates(pools.FabricFree, routing.PolicyFreeOnly) {
			if n.ProviderID == "prov-a" {
				return n.Score.Total
			}
		}
		t.Fatalf("prov-a missing from pool")
		return 0
	}

	// Build up to Trusted.
	for i := 0; i < 12; i++ {
		recordRouteOutcome("prov-a", "model-x", "conn-a", true, 0, nil)
	}
	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl != trust.TrustTrusted {
		t.Fatalf("expected Trusted after repeated success, got %s", lvl)
	}
	before := scoreFor()

	// A transient failure degrades it but keeps it routable.
	recordRouteOutcome("prov-a", "model-x", "conn-a", false, http.StatusInternalServerError, nil)
	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl != trust.TrustDegraded {
		t.Fatalf("expected Degraded after failure, got %s", lvl)
	}
	degraded := scoreFor()
	if degraded >= before {
		t.Fatalf("expected score to drop on degradation: before=%v degraded=%v", before, degraded)
	}

	// Recovery: consecutive successes restore it, and it must become
	// preferred again (score improves) rather than staying penalized.
	for i := 0; i < 4; i++ {
		recordRouteOutcome("prov-a", "model-x", "conn-a", true, 0, nil)
	}
	if lvl := globalTrustManager.GetTrustLevel("prov-a", "model-x", "conn-a"); lvl != trust.TrustVerified {
		t.Fatalf("expected Verified after recovery, got %s", lvl)
	}
	recovered := scoreFor()
	if recovered <= degraded {
		t.Fatalf("expected score to improve after recovery: degraded=%v recovered=%v", degraded, recovered)
	}
}

// --- full HTTP request/response wiring ---

func TestFabricFree_ChatCompletions_TransparentFailover(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	upstreamA := jsonErrorServer(http.StatusInternalServerError, "boom")
	defer upstreamA.Close()
	upstreamB := jsonSuccessServer("RESPONSE-FROM-B")
	defer upstreamB.Close()

	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", upstreamA.URL, "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", upstreamB.URL, "free")

	reqBody := `{"model":"fabric-free","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "RESPONSE-FROM-B") {
		t.Fatalf("expected transparent failover to B, got: %s", rec.Body.String())
	}
	// Note: a plain 500 is not in providers.RetryableStatusCodes, so the
	// per-entry connAttempt loop retries prov-a's single connection up to 10
	// times before moving on to B (existing pre-Fabric combo behavior, not
	// changed here) — that legitimately trips the repeated-failures
	// quarantine breaker within this one request. The "a single transient
	// failure must not quarantine" property is covered in isolation by
	// TestFabricFree_5xxAndTimeout_TransientDoesNotQuarantine below.
}

func TestFabricFree_Messages_TransparentFailover(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	upstreamA := jsonErrorServer(http.StatusInternalServerError, "boom")
	defer upstreamA.Close()
	upstreamB := jsonSuccessServer("RESPONSE-FROM-B")
	defer upstreamB.Close()

	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", upstreamA.URL, "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", upstreamB.URL, "free")

	reqBody := `{"model":"fabric-free","max_tokens":100,"messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader([]byte(reqBody)))
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "RESPONSE-FROM-B") {
		t.Fatalf("expected transparent failover to B via /v1/messages, got: %s", rec.Body.String())
	}
}

func TestFabricFree_TimeoutFailover(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	// A closed server simulates a transport-level failure (connection
	// refused), not an HTTP error status.
	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadServer.URL
	deadServer.Close()

	upstreamB := jsonSuccessServer("RESPONSE-FROM-B")
	defer upstreamB.Close()

	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", deadURL, "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", upstreamB.URL, "free")

	reqBody := `{"model":"fabric-free","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after failing over past a transport error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "RESPONSE-FROM-B") {
		t.Fatalf("expected transparent failover to B, got: %s", rec.Body.String())
	}
}

func TestFabricFree_AllCandidatesFailed_RetryAfterHonored(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	retryAt := "2099-01-01T00:00:05Z"
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"error":{"message":"rate limited","retryAfter":%q}}`, retryAt)
	}))
	defer upstreamA.Close()
	upstreamB := jsonErrorServer(http.StatusTooManyRequests, "also rate limited")
	defer upstreamB.Close()

	seedFabricRoute(t, database, "prov-a", "model-x", "conn-a", upstreamA.URL, "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", upstreamB.URL, "free")

	reqBody := `{"model":"fabric-free","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when every candidate is rate-limited, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected a Retry-After header to be surfaced to the caller")
	}
}

func TestFabricFree_StreamingPreCommitFailover(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	// A custom executor lets us fail deterministically before writing any
	// bytes — no real network timing involved.
	executor.Register("fabric-test-precommit-fail", func() executor.Executor {
		return func(w http.ResponseWriter, req *executor.Request) error {
			return fmt.Errorf("simulated pre-commit failure")
		}
	})

	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"STREAM-FROM-B\"}}]}\n\n"))
		flusher.Flush()
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer upstreamB.Close()

	seedFabricRoute(t, database, "fabric-test-precommit-fail", "model-x", "conn-a", "http://unused", "free")
	seedFabricRoute(t, database, "prov-b", "model-y", "conn-b", upstreamB.URL, "free")
	// Two otherwise-equal free candidates tie-break randomly; force A first
	// so this test deterministically exercises the pre-commit failover path
	// instead of occasionally succeeding via B on the first try.
	recordRouteOutcome("fabric-test-precommit-fail", "model-x", "conn-a", true, 0, nil)

	reqBody := `{"model":"fabric-free","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "STREAM-FROM-B") {
		t.Fatalf("expected failover to B's stream since A failed before commitment, got: %s", rec.Body.String())
	}
}

func TestFabricFree_NoFailoverAfterStreamCommitment(t *testing.T) {
	h, database, cleanup := fabricTestEnv(t)
	defer cleanup()

	var bCalls int32
	executor.Register("fabric-test-postcommit-fail", func() executor.Executor {
		return func(w http.ResponseWriter, req *executor.Request) error {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"PARTIAL-FROM-A\"}}]}\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Simulate the upstream connection dropping mid-stream, after
			// response bytes have already reached the client.
			return fmt.Errorf("simulated mid-stream upstream drop")
		}
	})
	executor.Register("fabric-test-postcommit-b", func() executor.Executor {
		return func(w http.ResponseWriter, req *executor.Request) error {
			atomic.AddInt32(&bCalls, 1)
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"SHOULD-NOT-APPEAR\"}}]}\n\n"))
			return nil
		}
	})

	seedFabricRoute(t, database, "fabric-test-postcommit-fail", "model-x", "conn-a", "http://unused", "free")
	seedFabricRoute(t, database, "fabric-test-postcommit-b", "model-y", "conn-b", "http://unused", "free")
	// Two otherwise-equal free candidates tie-break randomly; force A first
	// so this test deterministically exercises the post-commit hard-stop
	// instead of occasionally trying B first (where there'd be nothing to
	// terminate).
	recordRouteOutcome("fabric-test-postcommit-fail", "model-x", "conn-a", true, 0, nil)

	reqBody := `{"model":"fabric-free","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "PARTIAL-FROM-A") {
		t.Fatalf("expected A's partial output to reach the client, got: %s", body)
	}
	if strings.Contains(body, "SHOULD-NOT-APPEAR") {
		t.Fatalf("a second model's output was concatenated onto a committed response: %s", body)
	}
	if atomic.LoadInt32(&bCalls) != 0 {
		t.Fatalf("expected B to never be invoked once A's response was committed, got %d calls", bCalls)
	}
}

