package executor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy"
)

func TestForwardOpencodeMuseSparkPreserves403(t *testing.T) {
	const upstreamBody = `{"error":{"type":"FreeTierError","message":"free route denied"}}`
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonstream", true: "stream"}[stream], func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, upstreamBody)
			}))
			defer server.Close()

			cfg := providers.ProviderConfig{BaseURL: server.URL + "/inference/v1/chat/completions"}
			req := &Request{
				Ctx: context.Background(),
				Client: server.Client(),
				Config: &cfg,
				APIKey: "test-key",
				Body: []byte(`{"model":"muse-spark","messages":[{"role":"user","content":"hello"}]}`),
				IsStream: stream,
			}
			rec := httptest.NewRecorder()
			err := ForwardOpencode(rec, req)
			var upstreamErr *proxy.UpstreamError
			if !errors.As(err, &upstreamErr) {
				t.Fatalf("expected UpstreamError, got %T: %v", err, err)
			}
			if upstreamErr.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", upstreamErr.StatusCode)
			}
			if string(upstreamErr.Body) != upstreamBody {
				t.Fatalf("body = %q, want %q", upstreamErr.Body, upstreamBody)
			}
			if gotPath != "/inference/v1/responses" {
				t.Fatalf("path = %q, want /inference/v1/responses", gotPath)
			}
			if strings.Contains(rec.Body.String(), "free route denied") {
				t.Fatal("upstream error was written as successful response")
			}
		})
	}
}
