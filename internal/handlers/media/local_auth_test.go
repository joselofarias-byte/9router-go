package media

import (
	"net/http"
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
