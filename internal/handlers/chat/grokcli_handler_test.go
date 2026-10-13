package chat

import (
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy/executor"
)

func TestForwardGrokCLIRequest_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "grok-shell/1.0.45 (") {
			t.Errorf("expected current grok-shell User-Agent, got %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("x-grok-client-identifier") != "grok-shell" {
			t.Errorf("expected x-grok-client-identifier header, got %q", r.Header.Get("x-grok-client-identifier"))
		}
		if r.Header.Get("x-grok-client-version") != "1.0.45" {
			t.Errorf("expected x-grok-client-version 1.0.45, got %q", r.Header.Get("x-grok-client-version"))
		}
		if r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
			t.Errorf("expected X-XAI-Token-Auth=xai-grok-cli, got %q", r.Header.Get("X-XAI-Token-Auth"))
		}
		if r.Header.Get("x-authenticateresponse") != "authenticate-response" {
			t.Errorf("expected x-authenticateresponse header, got %q", r.Header.Get("x-authenticateresponse"))
		}
		if r.Header.Get("x-grok-client-mode") != "headless" {
			t.Errorf("expected x-grok-client-mode=headless, got %q", r.Header.Get("x-grok-client-mode"))
		}
		if r.Header.Get("x-grok-model-override") != "grok-build" {
			t.Errorf("expected x-grok-model-override=grok-build, got %q", r.Header.Get("x-grok-model-override"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization: Bearer test-key, got %q", r.Header.Get("Authorization"))
		}

		var reqBody map[string]any
		if err := json.UnmarshalRead(r.Body, &reqBody); err != nil {
			t.Fatalf("parse body: %v", err)
		}
		if reqBody["stream"] != true {
			t.Errorf("expected stream=true")
		}
		if reqBody["store"] != false {
			t.Errorf("expected store=false")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"grok response\"}\n\n" +
				"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n" +
				"data: [DONE]\n\n",
		))
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL: srv.URL,
	}
	body := []byte(`{"model":"grok-build","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	err := executor.ForwardGrokCLI(rec, &executor.Request{
		Client:   srv.Client(),
		Config:   cfg,
		APIKey:   "test-key",
		Body:     body,
		IsStream: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "grok response") {
		t.Errorf("expected response content, got %s", rec.Body.String())
	}
}

func TestForwardGrokCLIRequest_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL: srv.URL,
	}
	body := []byte(`{"model":"x","messages":[]}`)
	rec := httptest.NewRecorder()
	err := executor.ForwardGrokCLI(rec, &executor.Request{
		Client:   srv.Client(),
		Config:   cfg,
		APIKey:   "bad-key",
		Body:     body,
		IsStream: true,
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var ue *upstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *upstreamError, got %T", err)
	}
	if ue.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", ue.StatusCode)
	}
}

func TestGrokCLI_BaseURL_ResponsesEndpoint(t *testing.T) {
	cfg, ok := providers.KnownProviders["grok-cli"]
	if !ok {
		t.Fatal("expected grok-cli provider to be registered in KnownProviders")
	}
	expectedURL := "https://cli-chat-proxy.grok.com/v1/responses"
	if cfg.BaseURL != expectedURL {
		t.Errorf("expected grok-cli BaseURL %q, got %q", expectedURL, cfg.BaseURL)
	}
}

func TestForwardGrokCLIRequest_EndpointPath(t *testing.T) {
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n[DONE]\n\n"))
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL: srv.URL + "/v1/responses",
	}
	body := []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	err := executor.ForwardGrokCLI(rec, &executor.Request{
		Client:   srv.Client(),
		Config:   cfg,
		APIKey:   "test-key",
		Body:     body,
		IsStream: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPath != "/v1/responses" {
		t.Errorf("expected request path /v1/responses, got %q", receivedPath)
	}
}


func TestForwardGrokCLIRequest_ClientVersionOverrideAndModelHeader(t *testing.T) {
	t.Setenv("GROK_CLI_CLIENT_VERSION", "9.9.9-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-grok-client-version"); got != "9.9.9-test" {
			t.Fatalf("client version override = %q", got)
		}
		if got := r.Header.Get("x-grok-model-override"); got != "grok-4.6" {
			t.Fatalf("model override = %q", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "grok-shell/9.9.9-test (") {
			t.Fatalf("user agent = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\"}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{BaseURL: srv.URL}
	rec := httptest.NewRecorder()
	err := executor.ForwardGrokCLI(rec, &executor.Request{
		Client: srv.Client(), Config: cfg, APIKey: "test-key",
		Body: []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hi"}]}`),
		IsStream: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
