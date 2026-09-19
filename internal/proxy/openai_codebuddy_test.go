package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/providers"
)

func TestForwardOpenAICodeBuddyMirrorsAPIKeyHeader(t *testing.T) {
	const key = "ck_test.secret"
	var gotAuth, gotAPIKey, gotMarker, gotAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-API-Key")
		gotMarker = r.Header.Get("X-CodeBuddy-Request")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL:    srv.URL,
		AuthHeader: "Authorization",
		AuthScheme: "bearer",
		StaticHeaders: map[string]string{
			"x-codebuddy-request": "1",
		},
	}

	resp, err := ForwardOpenAI(context.Background(), srv.Client(), cfg, key, []byte(`{"model":"gpt-5.6-luna","stream":true}`), true)
	if err != nil {
		t.Fatalf("ForwardOpenAI() error = %v", err)
	}
	defer resp.Body.Close()

	if gotAuth != "Bearer "+key {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer "+key)
	}
	if gotAPIKey != key {
		t.Fatalf("X-API-Key = %q, want %q", gotAPIKey, key)
	}
	if gotMarker != "1" {
		t.Fatalf("X-CodeBuddy-Request = %q, want 1", gotMarker)
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", gotAccept)
	}
}

func TestForwardOpenAINonCodeBuddyDoesNotMirrorAPIKeyHeader(t *testing.T) {
	const key = "ordinary-key"
	var gotAPIKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL:    srv.URL,
		AuthHeader: "Authorization",
		AuthScheme: "bearer",
	}

	resp, err := ForwardOpenAI(context.Background(), srv.Client(), cfg, key, []byte(`{}`), false)
	if err != nil {
		t.Fatalf("ForwardOpenAI() error = %v", err)
	}
	defer resp.Body.Close()

	if gotAPIKey != "" {
		t.Fatalf("X-API-Key = %q, want empty for non-CodeBuddy provider", gotAPIKey)
	}
}
