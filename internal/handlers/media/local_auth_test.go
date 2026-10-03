package media

import (
	"9router/proxy/internal/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/handlers/chat"
	"9router/proxy/internal/providers"
)

func TestPrepareMediaClient_StripsGatewayKeyOnLocalNoAuth(t *testing.T) {
	h := &MediaHandler{ChatH: &chat.ChatHandler{Client: &http.Client{}}}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8080/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer local-gateway-key")
	req.Header.Set("X-Api-Key", "local-gateway-key")

	cfg := &providers.ProviderConfig{
		BaseURL:    "http://127.0.0.1:8080/v1/chat/completions",
		AuthHeader: "Authorization",
		AuthScheme: "bearer",
		NoAuth:     true,
		LocalOnly:  true,
	}
	if _, err := h.prepareMediaClient(req, cfg, "local", cfg.BaseURL, &chat.ConnectionData{}); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("gateway Authorization forwarded to local upstream: %q", got)
	}
	if got := req.Header.Get("X-Api-Key"); got != "" {
		t.Fatalf("gateway X-Api-Key forwarded to local upstream: %q", got)
	}
}

func TestHandleImages_LocalNoAuthStripsInboundGatewayKeys(t *testing.T) {
	database, cleanup := setupMultimodalTestDB(t)
	defer cleanup()
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Error("gateway credentials reached local server")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()
	original := providers.KnownProviders["llamacpp"]
	cfg := original
	cfg.BaseURL = upstream.URL + "/v1/chat/completions"
	providers.KnownProviders["llamacpp"] = cfg
	t.Cleanup(func() { providers.KnownProviders["llamacpp"] = original })
	h := newTestMediaHandler(db.NewRepo(database))
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"lc/local-model","prompt":"test"}`))
	req.Header.Set("Authorization", "Bearer gateway-only")
	req.Header.Set("X-Api-Key", "gateway-only")
	rec := httptest.NewRecorder()
	h.HandleImages(rec, req)
	if rec.Code != http.StatusOK || hits != 1 {
		t.Fatalf("status=%d hits=%d body=%s", rec.Code, hits, rec.Body.String())
	}
}
