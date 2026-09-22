package providers

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrNotLoopback is returned when a local-only provider is aimed at a host
// that is not a loopback address. The request must fail before any dial.
var ErrNotLoopback = errors.New("upstream URL is not a loopback address")

// IsLoopbackURL reports whether raw is an http(s) URL whose host is loopback.
// Private LAN addresses are not loopback: a local provider must not treat
// them as safe, because a tunnel or mis-typed host can sit in those ranges.
func IsLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" || u.User != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

// AssertLoopbackURL returns ErrNotLoopback when raw is not a loopback http(s) URL.
func AssertLoopbackURL(raw string) error {
	if IsLoopbackURL(raw) {
		return nil
	}
	host := raw
	if u, err := url.Parse(raw); err == nil && u != nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return fmt.Errorf("%w: host %q", ErrNotLoopback, host)
}

// IsLocalAPIType reports provider node apiType/type values that mean a local
// OpenAI-compatible server (llama-server or local Ollama). Cloud
// "openai-compatible" nodes are not included.
func IsLocalAPIType(apiType string) bool {
	switch ResolveAlias(strings.TrimSpace(apiType)) {
	case "llamacpp", "ollama-local":
		return true
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsPlaceholderLocalKey reports keys that mean "no credential" for a local
// server. A real llama-server --api-key is not a placeholder and may be sent,
// but only after the URL has already been locked to loopback.
func IsPlaceholderLocalKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "public", "local", "none", "noauth", "no-auth", "dummy":
		return true
	default:
		return false
	}
}

// LocalAuthConfig keeps NoAuth when the key is a placeholder. A non-placeholder
// key on an already LocalOnly config is forwarded to that loopback server.
// The function never attaches a key to a provider that was not already local-only.
func LocalAuthConfig(cfg *ProviderConfig, apiKey string) *ProviderConfig {
	if cfg == nil || !cfg.LocalOnly || !cfg.NoAuth {
		return cfg
	}
	if IsPlaceholderLocalKey(apiKey) {
		return cfg
	}
	cloned := *cfg
	cloned.NoAuth = false
	return &cloned
}
