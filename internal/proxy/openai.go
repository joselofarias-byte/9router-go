package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"9router/proxy/internal/providers"
)

// ForwardOpenAI sends an OpenAI-format request to the provider endpoint.
func ForwardOpenAI(ctx context.Context, client *http.Client, cfg *providers.ProviderConfig, apiKey string, body []byte, isStream bool) (*http.Response, error) {
	headers := map[string]string{}
	if !cfg.NoAuth {
		switch cfg.AuthScheme {
		case "bearer":
			headers[cfg.AuthHeader] = "Bearer " + apiKey
		case "raw":
			headers[cfg.AuthHeader] = apiKey
		default:
			headers["Authorization"] = "Bearer " + apiKey
		}
	}
	for k, v := range cfg.StaticHeaders {
		headers[k] = v
	}
	// CodeBuddy/WorkBuddy API keys are accepted as Bearer credentials by the
	// chat endpoint, while the official CLI also sends X-API-Key. Mirror both
	// forms whenever the provider is identified by its CodeBuddy request
	// marker. This keeps OAuth/Bearer compatibility and makes static ck_* API
	// keys work without introducing a second provider implementation.
	if apiKey != "" && hasHeaderKey(cfg.StaticHeaders, "x-codebuddy-request") {
		headers["X-API-Key"] = apiKey
	}
	if isStream {
		headers["Accept"] = "text/event-stream"
	}
	resp, err := DoRequest(ctx, client, "POST", cfg.BaseURL, headers, body)
	if err != nil {
		return nil, fmt.Errorf("forward to %s: %w", cfg.BaseURL, err)
	}
	return resp, nil
}

func hasHeaderKey(headers map[string]string, name string) bool {
	for k := range headers {
		if http.CanonicalHeaderKey(k) == http.CanonicalHeaderKey(name) {
			return true
		}
	}
	return false
}

// ReadBody reads and returns the response body (capped to prevent
// unbounded memory use), closing it.
func ReadBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

// UpstreamBody reads the body and wraps non-200 as UpstreamError.
func UpstreamBody(resp *http.Response) ([]byte, error) {
	body, err := ReadBody(resp)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: body}
	}
	return body, nil
}

// BuildURL joins a base URL with a path segment.
func BuildURL(base, path string) string {
	if base == "" {
		return path
	}
	if path == "" {
		return base
	}
	return fmt.Sprintf("%s/%s", base, path)
}
