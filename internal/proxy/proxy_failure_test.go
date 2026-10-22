package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsProxyFailureIgnoresApplication403(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("invalid api key")),
	}
	if isProxyFailure(nil, resp) {
		t.Fatal("application 403 text/plain was treated as a proxy refusal")
	}
	rest, err := io.ReadAll(resp.Body)
	if err != nil || string(rest) != "invalid api key" {
		t.Fatalf("body after classification = %q, %v", rest, err)
	}
}

func TestIsProxyFailureKeepsExplicitProxyRefusal(t *testing.T) {
	withHeader := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"X-Proxy-Error": []string{"allowlist"}, "Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"no"}`)),
	}
	if !isProxyFailure(nil, withHeader) {
		t.Fatal("X-Proxy-Error 403 was not a proxy refusal")
	}
	withBody := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader("blocked-by-allowlist")),
	}
	if !isProxyFailure(nil, withBody) {
		t.Fatal("allowlist body was not a proxy refusal")
	}
}

func TestDoRequestDoesNotReplayPlain403WithoutProxy(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer upstream.Close()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	_, err := DoRequest(context.Background(), client, http.MethodPost, upstream.URL, nil, []byte(`{}`))
	if hits.Load() != 1 {
		t.Fatalf("hits=%d, want 1", hits.Load())
	}
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.StatusCode != http.StatusForbidden {
		t.Fatalf("err=%v", err)
	}
}

func TestDoRequestReplaysProxyAllowlistOnDirect(t *testing.T) {
	var proxyHits, directHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked-by-allowlist"))
	}))
	defer proxySrv.Close()
	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := DoRequest(context.Background(), client, http.MethodPost, upstream.URL, nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if proxyHits.Load() != 1 || directHits.Load() != 1 {
		t.Fatalf("proxy=%d direct=%d", proxyHits.Load(), directHits.Load())
	}
}

func TestDoRequestDoesNotReplayApplication403FromProxy(t *testing.T) {
	var proxyHits, directHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer proxySrv.Close()
	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	_, err = DoRequest(context.Background(), client, http.MethodPost, upstream.URL, nil, []byte(`{}`))
	if proxyHits.Load() != 1 || directHits.Load() != 0 {
		t.Fatalf("proxy=%d direct=%d err=%v", proxyHits.Load(), directHits.Load(), err)
	}
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.StatusCode != http.StatusForbidden {
		t.Fatalf("err=%v", err)
	}
}
