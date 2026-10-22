package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"9router/proxy/internal/constants"
)

// UpstreamError captures a non-200 upstream response.
type UpstreamError struct {
	// EmptyCompletion distinguishes a model's unusable answer from an
	// account-wide outage without parsing human-readable error messages.
	EmptyCompletion bool
	StatusCode      int
	Body            []byte
	// Header is the upstream response's headers. Rate-limit carriers such as
	// Retry-After live only here — a Gemini/Antigravity 429 body repeats the
	// wait in a google.rpc.RetryInfo payload at best — and the error is built
	// where the response is still in hand, so this is the only chance to keep
	// them.
	Header http.Header
}

func (e *UpstreamError) Error() string {
	// Include the upstream body (truncated) so 4xx/5xx failures are diagnosable
	// from the fallback log alone — the body often carries Google/Antigravity's
	// actual rejection reason ("Invalid tool parameters", unknown model, etc.).
	body := strings.TrimSpace(string(e.Body))
	if strings.HasPrefix(body, "<!DOCTYPE html") || strings.HasPrefix(body, "<html") {
		lower := strings.ToLower(body)
		if strings.Contains(lower, "cloudflare") || strings.Contains(lower, "attention required") {
			return fmt.Sprintf("upstream returned %d: Cloudflare WAF challenge (Attention Required!): check User-Agent or network proxy", e.StatusCode)
		}
		if titleStart := strings.Index(lower, "<title>"); titleStart != -1 {
			titleEnd := strings.Index(lower[titleStart:], "</title>")
			if titleEnd != -1 {
				titleText := strings.TrimSpace(body[titleStart+7 : titleStart+titleEnd])
				return fmt.Sprintf("upstream returned %d: HTML page (%s)", e.StatusCode, titleText)
			}
		}
		return fmt.Sprintf("upstream returned %d: HTML error page", e.StatusCode)
	}
	if len(body) > 512 {
		body = body[:512] + "... (truncated)"
	}
	if body != "" {
		return fmt.Sprintf("upstream returned %d: %s", e.StatusCode, body)
	}
	return fmt.Sprintf("upstream returned %d", e.StatusCode)
}

var directProxyClient = &http.Client{
	Transport: constants.DefaultHTTPTransportConfig.NewTransport(),
}

func isProxyFailure(err error, resp *http.Response) bool {
	if resp != nil && resp.StatusCode == http.StatusForbidden {
		if resp.Header.Get("X-Proxy-Error") != "" {
			return true
		}
		// A bare text/plain 403 is an application response often enough that
		// replaying it would send the same prompt twice. Only a body that
		// names a proxy, tunnel, or allowlist block is a proxy refusal.
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/plain") && proxyRefusalBody(resp) {
			return true
		}
	}
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "proxy") || strings.Contains(errStr, "connect tunnel failed") || strings.Contains(errStr, "blocked-by-allowlist") {
			return true
		}
	}
	return false
}

func proxyRefusalBody(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	peeked, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body = PrefixReadCloser(peeked, resp.Body)
	if err != nil && len(peeked) == 0 {
		return false
	}
	return proxyRefusalText(string(peeked))
}

func proxyRefusalText(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{"proxy", "connect tunnel", "blocked-by-allowlist", "tunnel failed"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// requestUsesProxy reports whether this client would send req through a proxy.
// A direct client must not replay a 403: the second attempt is the same request.
func requestUsesProxy(client *http.Client, req *http.Request) bool {
	if client == nil || req == nil {
		return false
	}
	// FallbackTransport already replays a proxy refusal on its direct leg.
	// Replaying again here would send the same request twice.
	if _, already := client.Transport.(*FallbackTransport); already {
		return false
	}
	return roundTripperUsesProxy(client.Transport, req)
}

// PrefixReadCloser puts prefix back in front of rest and closes rest when
// the caller closes the result. io.NopCloser around a MultiReader drops that
// Close, so the original HTTP body would stay open.
func PrefixReadCloser(prefix []byte, rest io.ReadCloser) io.ReadCloser {
	return &prefixBody{prefix: bytes.NewReader(prefix), rest: rest}
}

type prefixBody struct {
	prefix *bytes.Reader
	rest   io.ReadCloser
}

func (b *prefixBody) Read(p []byte) (int, error) {
	if b.prefix != nil && b.prefix.Len() > 0 {
		n, err := b.prefix.Read(p)
		if err == io.EOF {
			// The prefix ending is not the body ending: rest still follows.
			err = nil
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	if b.rest == nil {
		return 0, io.EOF
	}
	return b.rest.Read(p)
}

func (b *prefixBody) Close() error {
	if b.rest == nil {
		return nil
	}
	return b.rest.Close()
}

func roundTripperUsesProxy(rt http.RoundTripper, req *http.Request) bool {
	if req == nil {
		return false
	}
	if rt == nil {
		rt = http.DefaultTransport
	}
	if ft, ok := rt.(*FallbackTransport); ok && ft != nil {
		rt = ft.Base
		if rt == nil {
			rt = http.DefaultTransport
		}
	}
	for {
		unwrapped, ok := rt.(interface{ Unwrap() http.RoundTripper })
		if !ok {
			break
		}
		next := unwrapped.Unwrap()
		if next == nil || next == rt {
			break
		}
		rt = next
	}
	ht, ok := rt.(*http.Transport)
	if !ok || ht.Proxy == nil {
		return false
	}
	proxyURL, err := ht.Proxy(req)
	return err == nil && proxyURL != nil
}

// DoRequest sends an HTTP POST to url with body and auth, returns the raw response.
// Caller must close resp.Body.
func DoRequest(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if isProxyFailure(err, resp) && requestUsesProxy(client, req) {
		if resp != nil {
			resp.Body.Close()
		}
		if directReq, dErr := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body)); dErr == nil {
			directReq.Header.Set("Content-Type", "application/json")
			for k, v := range headers {
				directReq.Header.Set(k, v)
			}
			if directResp, dErr2 := directProxyClient.Do(directReq); dErr2 == nil {
				resp = directResp
				err = nil
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		errBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("upstream returned %d and body read failed: %w", resp.StatusCode, readErr)
		}
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: errBody, Header: resp.Header}
	}
	return resp, nil
}
