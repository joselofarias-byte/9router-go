package providers

import "testing"

func TestLlamaLocalProvider_Defaults(t *testing.T) {
	cfg, ok := KnownProviders["llama-local"]
	if !ok {
		t.Fatal("expected llama-local provider to be registered")
	}
	if cfg.BaseURL != "http://127.0.0.1:8080/v1/chat/completions" {
		t.Fatalf("unexpected default llama-local BaseURL: %q", cfg.BaseURL)
	}
	if !cfg.NoAuth {
		t.Fatal("llama-local must not require an API key")
	}
	if cfg.AuthHeader != "Authorization" || cfg.AuthScheme != "bearer" {
		t.Fatalf("unexpected auth metadata: %s/%s", cfg.AuthHeader, cfg.AuthScheme)
	}
	if ResolveAlias("llama") != "llama-local" {
		t.Fatalf("llama alias did not resolve to llama-local")
	}
	if ResolveAlias("local") != "llama-local" {
		t.Fatalf("local alias did not resolve to llama-local")
	}
}

func TestLocalProviderURL_Override(t *testing.T) {
	t.Setenv("LLAMA_LOCAL_URL", "http://127.0.0.1:9090/v1/chat/completions")
	if got := localProviderURL(); got != "http://127.0.0.1:9090/v1/chat/completions" {
		t.Fatalf("expected env override, got %q", got)
	}
}
