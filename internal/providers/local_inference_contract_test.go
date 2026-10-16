package providers

import (
    "net"
    "net/url"
    "strings"
    "testing"
)

// This test runs unconditionally. It protects the existing local inference
// contract that a future Magnitude adapter should follow.
func TestLocalInferenceProvidersUseLoopback(t *testing.T) {
    for _, name := range []string{"llamacpp", "ollama-local"} {
        t.Run(name, func(t *testing.T) {
            cfg, ok := KnownProviders[name]
            if !ok {
                t.Fatalf("local provider %q missing", name)
            }
            if !cfg.LocalOnly {
                t.Fatal("local inference provider must set LocalOnly")
            }
            u, err := url.Parse(cfg.BaseURL)
            if err != nil {
                t.Fatalf("invalid BaseURL: %v", err)
            }
            if u.Scheme != "http" || u.User != nil {
                t.Fatalf("expected local HTTP URL without embedded credentials: %q", cfg.BaseURL)
            }
            host := u.Hostname()
            if host != "localhost" {
                ip := net.ParseIP(host)
                if ip == nil || !ip.IsLoopback() {
                    t.Fatalf("provider address is not loopback: %q", host)
                }
            }
            if !strings.HasSuffix(u.Path, "/v1/chat/completions") {
                t.Fatalf("not an OpenAI chat endpoint: %q", u.Path)
            }
        })
    }
}
