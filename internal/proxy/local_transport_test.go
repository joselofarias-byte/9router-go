package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/providers"
)

func TestForwardOpenAI_LocalOnlyRejectsCloudBeforeDial(t *testing.T) {
	cfg := &providers.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1/chat/completions",
		AuthHeader: "Authorization",
		AuthScheme: "bearer",
		NoAuth:     true,
		LocalOnly:  true,
	}
	_, err := ForwardOpenAI(context.Background(), http.DefaultClient, cfg, "sk-should-not-leak", []byte(`{"model":"x"}`), false)
	if err == nil {
		t.Fatal("expected cloud URL to be refused")
	}
	if !errors.Is(err, providers.ErrNotLoopback) {
		t.Fatalf("expected ErrNotLoopback, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-should-not-leak") {
		t.Fatalf("refusal leaked the api key: %v", err)
	}
}

func TestDialLoopback_RejectsPublicIP(t *testing.T) {
	_, err := dialLoopback(context.Background(), "tcp", "1.1.1.1:443")
	if !errors.Is(err, providers.ErrNotLoopback) {
		t.Fatalf("expected ErrNotLoopback, got %v", err)
	}
}

func TestDirectLoopbackClient_IgnoresEnvProxyAndRefusesRedirect(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")

	redirected := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("NoAuth local request sent Authorization %q", r.Header.Get("Authorization"))
			}
			http.Redirect(w, r, "https://example.com/v1/chat/completions", http.StatusFound)
			return
		}
		redirected = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL:    srv.URL + "/v1/chat/completions",
		AuthHeader: "Authorization",
		AuthScheme: "bearer",
		NoAuth:     true,
		LocalOnly:  true,
	}
	_, err := ForwardOpenAI(context.Background(), http.DefaultClient, cfg, "local", []byte(`{"model":"qwen"}`), true)
	if err == nil {
		t.Fatal("expected redirect off-machine to fail")
	}
	if !strings.Contains(err.Error(), "refused redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if redirected {
		t.Fatal("followed redirect onto a non-loopback host")
	}
}

func TestDirectLoopbackClient_ReachesLoopbackDespiteEnvProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"read_file"`) {
			t.Errorf("tool definition missing: %s", body)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("stream Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read_file\"}}]}}]}\n\n"))
	}))
	defer srv.Close()

	cfg := &providers.ProviderConfig{
		BaseURL:    srv.URL,
		NoAuth:     true,
		LocalOnly:  true,
		AuthHeader: "Authorization",
		AuthScheme: "bearer",
	}
	resp, err := ForwardOpenAI(context.Background(), &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}, cfg, "local", []byte(`{"tools":[{"function":{"name":"read_file"}}]}`), true)
	if err != nil {
		t.Fatalf("loopback forward failed: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(got), "call_1") || !strings.Contains(string(got), "read_file") {
		t.Fatalf("tool_calls stream not passed through: %s", got)
	}
}
