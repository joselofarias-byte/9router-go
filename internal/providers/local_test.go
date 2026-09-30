package providers

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestIsLoopbackURL(t *testing.T) {
	ok := []string{
		"http://127.0.0.1:8080/v1/chat/completions",
		"http://127.0.0.1:8080",
		"http://[::1]:8080/v1/chat/completions",
		"http://localhost:11434/v1/chat/completions",
		"http://localhost.:11434/v1/chat/completions",
		"https://llama.localhost/v1/chat/completions",
		"http://127.1.2.3:8080/v1/chat/completions",
	}
	for _, raw := range ok {
		if !IsLoopbackURL(raw) {
			t.Errorf("expected loopback: %s", raw)
		}
	}

	blocked := []string{
		"https://api.openai.com/v1/chat/completions",
		"https://ollama.com/api/web_fetch",
		"http://localhost.evil.com/v1/chat/completions",
		"http://evil.com/",
		"http://10.0.0.1:8080/v1/chat/completions",
		"http://192.168.1.2:8080/v1/chat/completions",
		"http://0.0.0.0:8080/v1/chat/completions",
		"file:///etc/passwd",
		"http://user:pass@127.0.0.1:8080/v1/chat/completions",
		"127.0.0.1:8080",
		"",
	}
	for _, raw := range blocked {
		if IsLoopbackURL(raw) {
			t.Errorf("expected non-loopback: %s", raw)
		}
		if err := AssertLoopbackURL(raw); !errors.Is(err, ErrNotLoopback) {
			t.Errorf("AssertLoopbackURL(%q) = %v, want ErrNotLoopback", raw, err)
		}
	}
}

func TestLlamaCppProvider_LocalNoAuth(t *testing.T) {
	cfg, ok := KnownProviders["llamacpp"]
	if !ok {
		t.Fatal("llamacpp provider not registered")
	}
	if !cfg.NoAuth || !cfg.LocalOnly {
		t.Fatalf("llamacpp must be NoAuth and LocalOnly, got %+v", cfg)
	}
	if cfg.FetchURL != "" {
		t.Fatalf("llamacpp must not have a cloud FetchURL, got %q", cfg.FetchURL)
	}
	if err := AssertLoopbackURL(cfg.BaseURL); err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://127.0.0.1:8080/v1/chat/completions" {
		t.Fatalf("unexpected llama-server URL %q", cfg.BaseURL)
	}
	if cfg.DefaultAPIKey == "" {
		t.Fatal("llamacpp needs a placeholder DefaultAPIKey so NoAuth forwarding can run without a stored connection")
	}

	local := KnownProviders["ollama-local"]
	if !local.NoAuth || !local.LocalOnly || local.FetchURL != "" {
		t.Fatalf("ollama-local must be local-only with no fetch URL, got %+v", local)
	}
	if err := AssertLoopbackURL(local.BaseURL); err != nil {
		t.Fatal(err)
	}

	// The mixed ollama provider intentionally fetches from ollama.com.
	// It must not be marked local-only, or that documented path would break.
	ollama := KnownProviders["ollama"]
	if ollama.LocalOnly {
		t.Fatal("ollama is the mixed local-chat/cloud-fetch provider and must not be LocalOnly")
	}
	if ollama.FetchURL != "https://ollama.com/api/web_fetch" {
		t.Fatalf("ollama FetchURL changed: %q", ollama.FetchURL)
	}

	for _, alias := range []string{"llama", "llama.cpp", "Llama.cpp", "llama-cpp", "llama-server", "gguf", "lc"} {
		if got := ResolveAlias(alias); got != "llamacpp" {
			t.Errorf("ResolveAlias(%q) = %q, want llamacpp", alias, got)
		}
	}
	if got := ResolveAlias("openai-compatible-chat-bn"); got != "openai-compatible-chat-bn" {
		t.Errorf("unknown node id must be preserved, got %q", got)
	}
	profile := GetProviderRiskProfile("llamacpp")
	if profile.AccessMode != AccessNoAuth {
		t.Fatalf("llamacpp risk profile = %+v, want no_auth", profile)
	}
}

func TestIsLocalAPIType(t *testing.T) {
	for _, apiType := range []string{"llamacpp", "llama.cpp", "llama-server", "gguf", "ollama-local", " LC "} {
		if !IsLocalAPIType(apiType) {
			t.Errorf("expected local api type %q", apiType)
		}
	}
	for _, apiType := range []string{"openai-compatible", "openai", "ollama", "anthropic", ""} {
		if IsLocalAPIType(apiType) {
			t.Errorf("expected non-local api type %q", apiType)
		}
	}
}

func TestIsPlaceholderLocalKey(t *testing.T) {
	for _, key := range []string{"", "public", "local", "NONE", " no-auth "} {
		if !IsPlaceholderLocalKey(key) {
			t.Errorf("expected placeholder: %q", key)
		}
	}
	if IsPlaceholderLocalKey("llama-server-secret") {
		t.Error("a real local server key must not be treated as a placeholder")
	}
}

func TestLocalAuthConfig_OnlyUnlocksLocalOnly(t *testing.T) {
	cloud := &ProviderConfig{BaseURL: "https://api.openai.com/v1/chat/completions", NoAuth: true}
	got := LocalAuthConfig(cloud, "sk-real")
	if !got.NoAuth {
		t.Fatal("a non-local provider must stay NoAuth even when a key is present")
	}

	local := &ProviderConfig{LocalOnly: true, NoAuth: true, BaseURL: "http://127.0.0.1:8080/v1/chat/completions"}
	if !LocalAuthConfig(local, "local").NoAuth {
		t.Fatal("placeholder key must keep NoAuth")
	}
	unlocked := LocalAuthConfig(local, "llama-server-secret")
	if unlocked.NoAuth {
		t.Fatal("real key on a local-only provider may be sent to loopback")
	}
	if !local.NoAuth {
		t.Fatal("LocalAuthConfig mutated the caller's config")
	}
	if !unlocked.LocalOnly {
		t.Fatal("unlocking auth must keep LocalOnly set")
	}
}

func TestLocalCloudFallbackPolicy_UnwiredAndCredentialFree(t *testing.T) {
	typ := reflect.TypeOf(LocalCloudFallbackPolicy{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, bad := range []string{"key", "token", "secret", "password", "credential", "auth", "proxy", "url"} {
			if strings.Contains(name, bad) {
				t.Fatalf("fallback policy field %s looks like a credential or endpoint", typ.Field(i).Name)
			}
		}
	}

	zero := LocalCloudFallbackPolicy{}
	if d := zero.Decide("unreachable"); d.Hop {
		t.Fatal("zero policy must not propose a hop")
	}

	enabledNoTarget := LocalCloudFallbackPolicy{Enabled: true, On: []string{"unreachable"}}
	if d := enabledNoTarget.Decide("unreachable"); d.Hop {
		t.Fatal("enabled policy without a provider id must not propose a hop")
	}

	policy := LocalCloudFallbackPolicy{
		Enabled:       true,
		CloudProvider: "openai",
		CloudModel:    "gpt-4.1-mini",
		On:            []string{"unreachable", "timeout", "http_5xx"},
	}
	for _, kind := range []string{"unreachable", "timeout", "http_5xx"} {
		d := policy.Decide(kind)
		if !d.Hop || d.CloudProvider != "openai" || d.CloudModel != "gpt-4.1-mini" {
			t.Fatalf("Decide(%s) = %+v", kind, d)
		}
	}
	for _, kind := range []string{"unauthorized", "http_4xx", "http_401", "quota", ""} {
		if d := policy.Decide(kind); d.Hop {
			t.Fatalf("Decide(%s) proposed a hop: %+v", kind, d)
		}
	}
	if d := policy.Decide("unreachable"); strings.Contains(d.CloudProvider+d.CloudModel+d.Reason, "sk-") {
		t.Fatal("decision must not carry a secret")
	}
}
