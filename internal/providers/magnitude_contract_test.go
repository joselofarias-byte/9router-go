package providers

import (
    "net"
    "net/url"
    "strings"
    "testing"
)

// TestMagnitudeLocalProviderContract is deliberately skipped until the upstream
// server address and connection registration have been verified. It documents
// the security and transport requirements without altering production routing.
func TestMagnitudeLocalProviderContract(t *testing.T) {
    cfg, ok := KnownProviders["magnitude"]
    if !ok {
        t.Skip("Magnitude not registered yet: waiting for confirmed server endpoint and connection/model wiring")
    }
    if !cfg.LocalOnly {
        t.Fatal("Magnitude local provider must enforce LocalOnly")
    }
    u, err := url.Parse(cfg.BaseURL)
    if err != nil || u.Scheme != "http" {
        t.Fatalf("expected local HTTP URL, got %q: %v", cfg.BaseURL, err)
    }
    host := u.Hostname()
    if host != "localhost" {
        ip := net.ParseIP(host)
        if ip == nil || !ip.IsLoopback() {
            t.Fatalf("non-loopback provider host: %q", host)
        }
    }
    if !strings.HasSuffix(u.Path, "/v1/chat/completions") {
        t.Fatalf("unexpected chat endpoint: %q", u.Path)
    }
    if !cfg.NoAuth {
        t.Log("Magnitude requires authentication; verify actual server configuration")
    }
}
