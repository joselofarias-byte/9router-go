package chat

import (
	"context"
	"database/sql"
	json "encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
)

func TestFreeBestSeveralAccountsFailThenNextFreeProvider(t *testing.T) {
	var paidHits, unknownHits atomic.Int32
	groqHits := make([]atomic.Int32, 3)
	var servers []*httptest.Server
	for i := range groqHits {
		i := i
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			groqHits[i].Add(1)
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		servers = append(servers, srv)
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"libre"}}]}`))
	}))
	servers = append(servers, ok)
	paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paidHits.Add(1)
		io.Copy(io.Discard, r.Body)
	}))
	unknown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unknownHits.Add(1)
		io.Copy(io.Discard, r.Body)
	}))
	servers = append(servers, paid, unknown)
	defer func() {
		for _, srv := range servers {
			srv.Close()
		}
	}()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'groq'`); err != nil {
		t.Fatal(err)
	}
	for i, srv := range []*httptest.Server{servers[0], servers[1], servers[2]} {
		seedConnDB(t, database, "groq", "groq-"+itoa(i), "gsk-"+itoa(i), srv.URL)
	}
	pointConnectionAt(t, database, "conn-1", "sk-ok", ok.URL)
	seedConnDB(t, database, "openai", "conn-paid", "sk-paid", paid.URL)
	seedConnDB(t, database, "ghost", "conn-unknown", "sk-unknown", unknown.URL)
	seedFreeRouteRegistry(t)
	state := registry.GetActiveState()
	state.Providers["openai"] = &registry.Provider{ID: "openai", IsActive: true}
	state.Providers["ghost"] = &registry.Provider{ID: "ghost", IsActive: true}
	state.ProviderModels["openai"] = map[string]*registry.ProviderModel{
		"gpt-4o": {ProviderID: "openai", ModelID: "gpt-4o", UpstreamModel: "gpt-4o", PricingMode: "paid", IsActive: true},
	}
	state.ProviderModels["ghost"] = map[string]*registry.ProviderModel{
		"mystery": {ProviderID: "ghost", ModelID: "mystery", UpstreamModel: "mystery", PricingMode: "unknown", IsActive: true},
	}
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.handleComboFallback(t.Context(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
		[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free", "openai/gpt-4o", "ghost/mystery"},
		"fallback", false, false, "free-best", 0, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "libre") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for i := range groqHits {
		if groqHits[i].Load() != 1 {
			t.Fatalf("groq account %d hits=%d, want 1", i, groqHits[i].Load())
		}
	}
	if paidHits.Load() != 0 || unknownHits.Load() != 0 {
		t.Fatalf("paid=%d unknown=%d", paidHits.Load(), unknownHits.Load())
	}
}

func TestFreeBest429HonorsRetryAfterWithoutSpinning(t *testing.T) {
	t.Run("sibling serves and the limited account is parked for the header", func(t *testing.T) {
		var limitedHits, spareHits atomic.Int32
		limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limitedHits.Add(1)
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"slow down"}}`))
		}))
		defer limited.Close()
		spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			spareHits.Add(1)
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"otra"}}]}`))
		}))
		defer spare.Close()
		database, cleanup := setupChatTestDB(t)
		defer cleanup()
		if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'groq'`); err != nil {
			t.Fatal(err)
		}
		seedConnDB(t, database, "groq", "groq-limited", "gsk-limited", limited.URL)
		seedConnDB(t, database, "groq", "groq-spare", "gsk-spare", spare.URL)
		if _, err := database.Exec(`UPDATE providerConnections SET priority = 2 WHERE id = 'groq-spare'`); err != nil {
			t.Fatal(err)
		}
		seedFreeRouteRegistry(t)
		h := NewChatHandler(db.NewRepo(database))
		start := time.Now()
		rec := httptest.NewRecorder()
		h.handleComboFallback(context.Background(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
			[]string{"groq/llama-3.1-8b-instant"}, "fallback", false, false, "free-best", 0, true)
		if time.Since(start) > 5*time.Second {
			t.Fatalf("429 failover waited %s", time.Since(start))
		}
		if rec.Code != http.StatusOK || limitedHits.Load() != 1 || spareHits.Load() != 1 {
			t.Fatalf("status=%d limited=%d spare=%d body=%s", rec.Code, limitedHits.Load(), spareHits.Load(), rec.Body.String())
		}
		until := rateLimitedUntil(t, database, "groq-limited")
		wait := time.Until(until)
		if wait < 90*time.Second || wait > 150*time.Second {
			t.Fatalf("rateLimitedUntil wait=%s, want about 120s", wait)
		}
	})

	t.Run("every account limited is one hop each and the client is told to wait", func(t *testing.T) {
		var hits atomic.Int32
		seen := map[string]int{}
		var mu sync.Mutex
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			mu.Lock()
			seen[r.Header.Get("Authorization")]++
			mu.Unlock()
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "90")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"slow down"}}`))
		}))
		defer upstream.Close()
		database, cleanup := setupChatTestDB(t)
		defer cleanup()
		if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
			t.Fatal(err)
		}
		seedConnDB(t, database, "groq", "groq-a", "gsk-a", upstream.URL)
		seedConnDB(t, database, "groq", "groq-b", "gsk-b", upstream.URL)
		seedFreeRouteRegistry(t)
		h := NewChatHandler(db.NewRepo(database))
		start := time.Now()
		rec := httptest.NewRecorder()
		h.handleComboFallback(context.Background(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
			[]string{"groq/llama-3.1-8b-instant"}, "fallback", false, false, "free-best", 0, true)
		if time.Since(start) > 5*time.Second {
			t.Fatalf("exhausted 429 waited %s", time.Since(start))
		}
		if hits.Load() != 2 {
			t.Fatalf("hits=%d, want one per account", hits.Load())
		}
		mu.Lock()
		defer mu.Unlock()
		for key, n := range seen {
			if n != 1 {
				t.Fatalf("account %s called %d times", key, n)
			}
		}
		if rec.Header().Get("Retry-After") != "90" {
			t.Fatalf("Retry-After=%q body=%s", rec.Header().Get("Retry-After"), rec.Body.String())
		}
	})
}

func TestFreeBest403TextPlainIsOneAccountHop(t *testing.T) {
	var deniedHits, okHits atomic.Int32
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deniedHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("invalid api key"))
	}))
	defer denied.Close()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		okHits.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"sigue"}}]}`))
	}))
	defer ok.Close()
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	pointConnectionAt(t, database, "conn-2", "gsk-denied", denied.URL)
	pointConnectionAt(t, database, "conn-1", "sk-ok", ok.URL)
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.handleComboFallback(context.Background(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
		[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"},
		"fallback", false, false, "free-best", 0, true)
	if deniedHits.Load() != 1 || okHits.Load() != 1 || rec.Code != http.StatusOK {
		t.Fatalf("403 text/plain hits denied=%d ok=%d status=%d body=%s", deniedHits.Load(), okHits.Load(), rec.Code, rec.Body.String())
	}
}

func TestFreeBestClientCancelDoesNotWalkThePool(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "openai"
		call := func(h *ChatHandler, ctx context.Context, rec *httptest.ResponseRecorder) {
			h.handleComboFallback(ctx, rec, []byte(`{"model":"free-best","stream":true,"messages":[{"role":"user","content":"hola"}]}`),
				[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"},
				"fallback", stream, false, "free-best", 0, true)
		}
		if stream {
			name = "anthropic"
			call = func(h *ChatHandler, ctx context.Context, rec *httptest.ResponseRecorder) {
				h.handleMessagesComboFallback(ctx, rec, map[string]any{
					"model": "free-best", "max_tokens": 16, "stream": true,
					"messages": []any{map[string]any{"role": "user", "content": "hola"}},
				}, []string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"}, "fallback", true, "free-best", 0, true)
			}
		}
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			pointConnectionAt(t, database, "conn-2", "gsk", upstream.URL)
			pointConnectionAt(t, database, "conn-1", "sk", upstream.URL)
			seedFreeRouteRegistry(t)
			h := NewChatHandler(db.NewRepo(database))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			call(h, ctx, httptest.NewRecorder())
			if hits.Load() != 0 {
				t.Fatalf("canceled caller still dialed upstream %d times", hits.Load())
			}
		})
	}
}

func TestComboPartialStreamIsNotReplayed(t *testing.T) {
	cases := []struct {
		name   string
		stream func(h *ChatHandler, rec *httptest.ResponseRecorder)
	}{
		{
			name: "openai",
			stream: func(h *ChatHandler, rec *httptest.ResponseRecorder) {
				h.handleComboFallback(context.Background(), rec, []byte(`{"model":"free-best","stream":true,"messages":[{"role":"user","content":"hola"}]}`),
					[]string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"},
					"fallback", true, false, "free-best", 0, true)
			},
		},
		{
			name: "anthropic",
			stream: func(h *ChatHandler, rec *httptest.ResponseRecorder) {
				h.handleMessagesComboFallback(context.Background(), rec, map[string]any{
					"model": "free-best", "max_tokens": 16, "stream": true,
					"messages": []any{map[string]any{"role": "user", "content": "hola"}},
				}, []string{"groq/llama-3.1-8b-instant", "deepseek/deepseek-reasoner-free"}, "fallback", true, "free-best", 0, true)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var secondHits atomic.Int32
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Content-Length", "9999")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"parcial\"}}]}\n\n"))
				w.(http.Flusher).Flush()
			}))
			defer first.Close()
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondHits.Add(1)
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer second.Close()
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			pointConnectionAt(t, database, "conn-2", "gsk-first", first.URL)
			pointConnectionAt(t, database, "conn-1", "sk-second", second.URL)
			seedFreeRouteRegistry(t)
			h := NewChatHandler(db.NewRepo(database))
			rec := httptest.NewRecorder()
			tc.stream(h, rec)
			if secondHits.Load() != 0 || !strings.Contains(rec.Body.String(), "parcial") {
				t.Fatalf("partial %s replayed: second=%d body=%s", tc.name, secondHits.Load(), rec.Body.String())
			}
		})
	}
}

func TestFreeBestAccountWalkIsBounded(t *testing.T) {
	var hits atomic.Int32
	seen := map[string]int{}
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mu.Lock()
		seen[r.Header.Get("Authorization")]++
		mu.Unlock()
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"no"}}`))
	}))
	defer upstream.Close()
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		seedConnDB(t, database, "groq", "groq-"+itoa(i), "gsk-"+itoa(i), upstream.URL)
	}
	seedFreeRouteRegistry(t)
	h := NewChatHandler(db.NewRepo(database))
	start := time.Now()
	rec := httptest.NewRecorder()
	h.handleComboFallback(context.Background(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hola"}]}`),
		[]string{"groq/llama-3.1-8b-instant"}, "fallback", false, false, "free-best", 0, true)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("account walk took %s", time.Since(start))
	}
	if hits.Load() > 10 {
		t.Fatalf("hits=%d, the per-model cap is 10", hits.Load())
	}
	if hits.Load() == 0 {
		t.Fatal("no account was tried")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != int(hits.Load()) {
		t.Fatalf("hits=%d unique=%d, an account was repeated: %#v", hits.Load(), len(seen), seen)
	}
	for key, n := range seen {
		if n != 1 {
			t.Fatalf("%s called %d times", key, n)
		}
	}
}

func rateLimitedUntil(t *testing.T, database *sql.DB, id string) time.Time {
	t.Helper()
	var raw string
	if err := database.QueryRow(`SELECT data FROM providerConnections WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var data struct {
		RateLimitedUntil string `json:"rateLimitedUntil"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse(time.RFC3339, data.RateLimitedUntil)
	if err != nil {
		t.Fatalf("rateLimitedUntil %q: %v", data.RateLimitedUntil, err)
	}
	return until
}
