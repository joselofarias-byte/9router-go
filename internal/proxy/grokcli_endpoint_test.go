package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/providers"
)

// TestFabricGrokCLIEndpoint verifies ForwardGrokCLI normalizes a bare/​"/v1"
// base URL to "/v1/responses" (the CLI backend does not serve the root path),
// leaves an already-specific custom path untouched, and sends the CLI-parity
// headers the backend uses to route/authorize the request.
func TestFabricGrokCLIEndpoint(t *testing.T) {
	for _, path := range []string{"", "/", "/v1", "/v1/", "/v1/responses", "/custom/responses"} {
		t.Run(path, func(t *testing.T) {
			want := "/v1/responses"
			if path == "/custom/responses" {
				want = path
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != want || r.Method != "POST" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
					t.Error("missing CLI auth header")
				}
				if r.Header.Get("x-grok-model-override") != "grok-build" {
					t.Error("missing model header")
				}
				if r.Header.Get("Authorization") != "Bearer test-only" {
					t.Error("missing bearer header")
				}
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"error":"route or model missing"}`)
			}))
			defer s.Close()
			cfg := &providers.ProviderConfig{BaseURL: s.URL + path, AuthHeader: "Authorization", AuthScheme: "bearer"}
			_, err := ForwardGrokCLI(context.Background(), s.Client(), cfg, "test-only", []byte(`{"model":"grok-build","input":[],"stream":true}`), true)
			if ue, ok := err.(*UpstreamError); !ok || ue.StatusCode != 404 {
				t.Fatalf("expected preserved upstream 404, got %T: %v", err, err)
			}
		})
	}
}
