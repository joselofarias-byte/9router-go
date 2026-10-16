package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/db"
)

func TestAccountFallbackTransientUsesHealthyAccount(t *testing.T) {
	for _, failure := range []int{0, 500, 502, 503, 504, 400} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			var failedHits, healthyHits atomic.Int32
			failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				failedHits.Add(1)
				w.WriteHeader(failure)
				w.Write([]byte(`{"error":{"message":"upstream failed"}}`))
			}))
			defer failed.Close()
			if failure == 0 {
				failed.Close() // Real connection-refused transport error.
			}
			healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				healthyHits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"healthy","choices":[{"message":{"content":"ok"}}]}`))
			}))
			defer healthy.Close()
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'deepseek'`); err != nil {
				t.Fatal(err)
			}
			firstID := fmt.Sprintf("transient-first-%d", failure)
			secondID := fmt.Sprintf("transient-second-%d", failure)
			seedConnDB(t, database, "deepseek", firstID, "sk-first", failed.URL)
			seedConnDB(t, database, "deepseek", secondID, "sk-second", healthy.URL)
			if _, err := database.Exec(`UPDATE providerConnections SET priority = 2 WHERE id = ?`, secondID); err != nil {
				t.Fatal(err)
			}
			h := NewChatHandler(db.NewRepo(database))
			rec := httptest.NewRecorder()
			body := []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`)
			err := h.handleAccountFallback(context.Background(), rec, "deepseek", "deepseek-chat", "", body, false, false, "/v1/chat/completions")
			if failure == 400 {
				if err == nil || healthyHits.Load() != 0 {
					t.Fatalf("bad request must stop: err=%v healthy hits=%d", err, healthyHits.Load())
				}
				return
			}
			if err != nil || healthyHits.Load() != 1 || !strings.Contains(rec.Body.String(), "healthy") {
				t.Fatalf("healthy fallback missing: err=%v healthy hits=%d body=%s", err, healthyHits.Load(), rec.Body.String())
			}
			if failure != 0 && failedHits.Load() != 1 {
				t.Fatalf("expected failed account exactly once, got %d", failedHits.Load())
			}
		})
	}
}

func TestAccountFallbackCanceledDoesNotCallUpstream(t *testing.T) {
	h, cleanup := setupHandlerForForward(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := h.handleAccountFallback(ctx, httptest.NewRecorder(), "deepseek", "deepseek-chat", "", []byte(`{}`), false, false, "/v1/chat/completions")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation, got %v", err)
	}
}

func TestCommittedWriterFlushMarksResponseStarted(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := newCommittedResponseWriter(rec)
	cw.Flush()
	if !cw.IsCommitted() || !rec.Flushed {
		t.Fatal("flush sends HTTP headers and must prevent fallback")
	}
}

func TestAccountFallbackDoesNotReplayPartialStream(t *testing.T) {
	var healthyHits atomic.Int32
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "9999")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer broken.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits.Add(1)
		w.Write([]byte(`{"id":"unexpected-replay"}`))
	}))
	defer healthy.Close()
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'deepseek'`); err != nil {
		t.Fatal(err)
	}
	seedConnDB(t, database, "deepseek", "partial-stream-first", "sk-first", broken.URL)
	seedConnDB(t, database, "deepseek", "partial-stream-second", "sk-second", healthy.URL)
	if _, err := database.Exec(`UPDATE providerConnections SET priority = 2 WHERE id = 'partial-stream-second'`); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"deepseek-chat","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	err := h.handleAccountFallback(context.Background(), rec, "deepseek", "deepseek-chat", "", body, true, false, "/v1/chat/completions")
	if err == nil || healthyHits.Load() != 0 || !strings.Contains(rec.Body.String(), "partial") {
		t.Fatalf("partial stream must not replay: err=%v healthy hits=%d body=%s", err, healthyHits.Load(), rec.Body.String())
	}
}
