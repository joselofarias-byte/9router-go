package chat

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/db"
)

// The virtual route must not be advertised if no free candidate has
// an active local connection. An empty catalog is safer than a dead route.
func TestHandleModels_FreeBestNotAdvertisedWithoutRoutes(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	h := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"id":"free-best"`)) {
		t.Fatalf("advertised free-best without a routable free candidate: %s", rec.Body.String())
	}
}
