package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
)

// Upstream's capacity adapter can prepend paid models to ordinary combos.
// Virtual free pools must retain their provenance through every entry point.
func TestVirtualFreePool_CapacityAdapterCannotInjectPaidProvider(t *testing.T) {
	for _, endpoint := range []string{"chat", "messages", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			seedFreeRouteRegistry(t)
			var paidHits atomic.Int32
			free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer free.Close()
			paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paidHits.Add(1)
				http.Error(w, "paid provider must not run", http.StatusPaymentRequired)
			}))
			defer paid.Close()
			pointConnectionAt(t, database, "conn-1", "test-free", free.URL)
			pointConnectionAt(t, database, "conn-2", "test-free", free.URL)
			seedConnDB(t, database, "antigravity", "paid-adapter", "test-paid", paid.URL)
			_, err := database.Exec(`UPDATE settings SET data = ? WHERE id = 1`, `{"capacityAdapter":{"vision":{"enabled":true,"models":["ag/gemini-3.8-flash-high"]}}}`)
			if err != nil {
				t.Fatal(err)
			}
			h := NewChatHandler(db.NewRepo(database))
			content := `[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]`
			body := `{"model":"free-best","messages":[{"role":"user","content":` + content + `}]}`
			switch endpoint {
			case "messages":
				body = `{"model":"free-best","max_tokens":16,"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`
			case "responses":
				body = `{"model":"free-best","input":[{"role":"user","content":[{"type":"input_text","text":"hi"},{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
			switch endpoint {
			case "chat":
				h.HandleChatCompletions(rec, req)
			case "messages":
				h.HandleMessages(rec, req)
			case "responses":
				h.HandleResponses(rec, req)
			}
			if paidHits.Load() != 0 {
				t.Fatalf("capacity adapter called a paid provider %d times", paidHits.Load())
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleResponses_EmptyVirtualFreePoolFailsClosed(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	registry.InitRegistry(nil)
	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"free","input":"hi"}`)))
	assertFreeRouteUnavailable(t, rec)
}
