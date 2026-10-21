package chat

import (
	"context"
	json "encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
)

func TestHandleModels_AdvertisesVirtualRoutesOnlyWhenRoutable(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, id := range virtualFreeModelIDs() {
		if !strings.Contains(rec.Body.String(), `"`+id+`"`) {
			t.Errorf("routable virtual model %s missing from %s", id, rec.Body.String())
		}
	}
}

func TestFreeBestFailoverMatrix(t *testing.T) {
	cases := []struct {
		name   string
		status int
		close  bool
	}{
		{"401", http.StatusUnauthorized, false},
		{"403", http.StatusForbidden, false},
		{"402", http.StatusPaymentRequired, false},
		{"429", http.StatusTooManyRequests, false},
		{"500", http.StatusInternalServerError, false},
		{"503", http.StatusServiceUnavailable, false},
		{"transport", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var failedHits, okHits, paidHits atomic.Int32
			failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				failedHits.Add(1)
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"error":{"message":"upstream failed","type":"upstream_error"}}`))
			}))
			defer failed.Close()
			if tc.close {
				failed.Close()
			}
			var okModel atomic.Value
			ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				okHits.Add(1)
				body, _ := io.ReadAll(r.Body)
				var req struct {
					Model string `json:"model"`
				}
				_ = json.Unmarshal(body, &req)
				okModel.Store(req.Model)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"siguiente"}}]}`))
			}))
			defer ok.Close()
			paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paidHits.Add(1)
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusPaymentRequired)
			}))
			defer paid.Close()

			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			pointConnectionAt(t, database, "conn-2", "gsk-test-groq-key", failed.URL)
			pointConnectionAt(t, database, "conn-1", "sk-test-deepseek-key", ok.URL)
			seedConnDB(t, database, "openai", "conn-paid", "sk-paid", paid.URL)
			seedFreeRouteRegistry(t)
			state := registry.GetActiveState()
			state.Providers["openai"] = &registry.Provider{ID: "openai", IsActive: true}
			state.ProviderModels["openai"] = map[string]*registry.ProviderModel{
				"gpt-4o": {ProviderID: "openai", ModelID: "gpt-4o", UpstreamModel: "gpt-4o", PricingMode: "paid", IsActive: true},
			}
			h := NewChatHandler(db.NewRepo(database))
			rec := httptest.NewRecorder()
			body := []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`)
			h.handleComboFallback(t.Context(), rec, body,
				[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free", "openai/gpt-4o"},
				"fallback", false, false, "free-best", 0, true)
			gotModel, _ := okModel.Load().(string)
			if rec.Code != http.StatusOK || okHits.Load() != 1 || paidHits.Load() != 0 || gotModel != "deepseek-reasoner-free" || !strings.Contains(rec.Body.String(), "siguiente") {
				t.Fatalf("failover %s: status=%d failed=%d ok=%d paid=%d model=%q body=%s", tc.name, rec.Code, failedHits.Load(), okHits.Load(), paidHits.Load(), gotModel, rec.Body.String())
			}
			if !tc.close && failedHits.Load() != 1 {
				t.Fatalf("failed provider hits=%d, want 1", failedHits.Load())
			}
		})
	}
}

func TestFreeBestPermanentRequestDoesNotBurnSiblingAccount(t *testing.T) {
	var firstHits, spareHits, nextHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"malformed"}}`))
	}))
	defer first.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spareHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer spare.Close()
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"otro proveedor"}}]}`))
	}))
	defer next.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	pointConnectionAt(t, database, "conn-2", "gsk-first", first.URL)
	seedConnDB(t, database, "groq", "conn-groq-spare", "gsk-spare", spare.URL)
	if _, err := database.Exec(`UPDATE providerConnections SET priority = 2 WHERE id = 'conn-groq-spare'`); err != nil {
		t.Fatal(err)
	}
	pointConnectionAt(t, database, "conn-1", "sk-next", next.URL)
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.handleComboFallback(t.Context(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
		[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"},
		"fallback", false, false, "free-best", 0, true)
	if firstHits.Load() != 1 || spareHits.Load() != 0 || nextHits.Load() != 1 || rec.Code != http.StatusOK {
		t.Fatalf("400 rotation: first=%d spare=%d next=%d status=%d body=%s", firstHits.Load(), spareHits.Load(), nextHits.Load(), rec.Code, rec.Body.String())
	}
}

func TestFreeBestAuthRotatesSiblingAccount(t *testing.T) {
	var firstHits, spareHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad credentials"}}`))
	}))
	defer first.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spareHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"otra cuenta"}}]}`))
	}))
	defer spare.Close()
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	pointConnectionAt(t, database, "conn-2", "gsk-first", first.URL)
	seedConnDB(t, database, "groq", "conn-groq-spare", "gsk-spare", spare.URL)
	if _, err := database.Exec(`UPDATE providerConnections SET priority = 2 WHERE id = 'conn-groq-spare'`); err != nil {
		t.Fatal(err)
	}
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.handleComboFallback(t.Context(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
		[]string{"groq/llama-3.1-8b-instant"},
		"fallback", false, false, "free-best", 0, true)
	if rec.Code != http.StatusOK || firstHits.Load() != 1 || spareHits.Load() != 1 || !strings.Contains(rec.Body.String(), "otra cuenta") {
		t.Fatalf("401 account failover: status=%d first=%d spare=%d body=%s", rec.Code, firstHits.Load(), spareHits.Load(), rec.Body.String())
	}
	var raw string
	if err := database.QueryRow(`SELECT data FROM providerConnections WHERE id = 'conn-2'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "rateLimitedUntil") {
		t.Fatalf("401 did not cool down the failed account: %s", raw)
	}
}

func TestAccountFallbackQuota402RotatesAnd500DoesNotLock(t *testing.T) {
	for _, status := range []int{http.StatusPaymentRequired, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(status)
				w.Write([]byte(`{"error":{"message":"no"}}`))
			}))
			defer first.Close()
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"ok"}}]}`))
			}))
			defer second.Close()
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'deepseek'`); err != nil {
				t.Fatal(err)
			}
			seedConnDB(t, database, "deepseek", "acct-a", "sk-a", first.URL)
			seedConnDB(t, database, "deepseek", "acct-b", "sk-b", second.URL)
			if _, err := database.Exec(`UPDATE providerConnections SET priority = 2 WHERE id = 'acct-b'`); err != nil {
				t.Fatal(err)
			}
			h := NewChatHandler(db.NewRepo(database))
			rec := httptest.NewRecorder()
			err := h.handleAccountFallback(context.Background(), rec, "deepseek", "deepseek-chat", "", []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`), false, false, "/v1/chat/completions")
			if err != nil || !strings.Contains(rec.Body.String(), "ok") {
				t.Fatalf("status %d fallback: err=%v body=%s", status, err, rec.Body.String())
			}
			var raw string
			if err := database.QueryRow(`SELECT data FROM providerConnections WHERE id = 'acct-a'`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			locked := strings.Contains(raw, "rateLimitedUntil")
			if status == http.StatusPaymentRequired && !locked {
				t.Fatalf("402 must record quota cooldown: %s", raw)
			}
			if status == http.StatusInternalServerError && locked {
				t.Fatalf("500 must not record quota cooldown: %s", raw)
			}
		})
	}
}

func TestMessagesComboDoesNotReplayPartialStream(t *testing.T) {
	var secondHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"parcial\"}}]}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Write([]byte(`{"id":"replay"}`))
	}))
	defer second.Close()
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	pointConnectionAt(t, database, "conn-1", "sk-first", first.URL)
	pointConnectionAt(t, database, "conn-2", "gsk-second", second.URL)
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.handleMessagesComboFallback(context.Background(), rec, map[string]any{
		"model": "free-best", "max_tokens": 16,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, []string{"deepseek/deepseek-chat", "groq/llama-3.1-8b-instant"}, "fallback", true, "free-best", 0, false)
	if secondHits.Load() != 0 || !strings.Contains(rec.Body.String(), "parcial") {
		t.Fatalf("partial messages replay: second=%d body=%s", secondHits.Load(), rec.Body.String())
	}
}

func TestFreeBestPerHopTimeoutUsesNextProvider(t *testing.T) {
	var slowHits, fastHits atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slowHits.Add(1)
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"a tiempo"}}]}`))
	}))
	defer fast.Close()
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	pointConnectionAt(t, database, "conn-2", "gsk-slow", slow.URL)
	pointConnectionAt(t, database, "conn-1", "sk-fast", fast.URL)
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	h.Client = &http.Client{Timeout: 200 * time.Millisecond}
	rec := httptest.NewRecorder()
	h.handleComboFallback(t.Context(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
		[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"},
		"fallback", false, false, "free-best", 0, true)
	if rec.Code != http.StatusOK || slowHits.Load() != 1 || fastHits.Load() != 1 || !strings.Contains(rec.Body.String(), "a tiempo") {
		t.Fatalf("timeout failover: status=%d slow=%d fast=%d body=%s", rec.Code, slowHits.Load(), fastHits.Load(), rec.Body.String())
	}
}

func TestExplainFreeRouteOmitsSecrets(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedFreeRouteRegistry(t)
	const secret = "sk-super-secret-token"
	if _, err := database.Exec(`UPDATE providerConnections SET data = json_set(data, '$.apiKey', ?, '$.lastError', json(?)) WHERE id = 'conn-2'`,
		secret, `{"status":401,"message":"Bearer `+secret+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE providerConnections SET data = json_set(data, '$.rateLimitedUntil', ?) WHERE id = 'conn-2'`,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(db.NewRepo(database))
	diag := h.ExplainFreeRoute("free-best")
	raw := mustJSON(t, diag)
	if strings.Contains(raw, secret) || strings.Contains(raw, "gsk-test") || strings.Contains(raw, "Bearer") || strings.Contains(raw, "apiKey") {
		t.Fatalf("diagnosis leaked credential material: %s", raw)
	}
	if !diag.Routable {
		t.Fatal("expected a routable free-best pool")
	}
	found := map[string]bool{}
	for _, row := range diag.Discarded {
		found[row.Reason+" "+row.Model] = true
	}
	if !found["paid deepseek-chat"] || !found["unknown_cost not-classified"] {
		t.Fatalf("discards = %#v", diag.Discarded)
	}
	var groqQuota string
	for _, hop := range diag.Selected {
		if hop.Provider == "groq" {
			groqQuota = hop.Quota
		}
	}
	if groqQuota != "auth_failure" {
		t.Fatalf("groq quota = %q, want auth_failure; selected=%#v", groqQuota, diag.Selected)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/free-best?model=gpt-4", nil)
	h.HandleFreeBestDiagnosis(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-virtual diagnosis status=%d", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
