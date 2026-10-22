package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/proxy"
)

type closeLog struct {
	io.Reader
	closes int
}

func (c *closeLog) Close() error {
	c.closes++
	return nil
}

func TestFreebuffRefusalPeekClosesOriginalBody(t *testing.T) {
	short := &closeLog{Reader: strings.NewReader("invalid api key")}
	longBody := "blocked-by-allowlist\n" + strings.Repeat("y", 4000)
	long := &closeLog{Reader: strings.NewReader(longBody)}
	cases := []struct {
		name    string
		body    *closeLog
		payload string
		refuse  bool
	}{
		{name: "short application 403", body: short, payload: "invalid api key", refuse: false},
		{name: "long proxy refusal", body: long, payload: longBody, refuse: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       tc.body,
			}
			if got := isFreebuffProxyRefusal(nil, resp); got != tc.refuse {
				t.Fatalf("isFreebuffProxyRefusal=%v, want %v", got, tc.refuse)
			}
			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.payload {
				t.Fatalf("body len=%d, want %d", len(got), len(tc.payload))
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if tc.body.closes != 1 {
				t.Fatalf("original Close calls=%d, want 1", tc.body.closes)
			}
		})
	}
}

type scriptRT struct {
	inner   http.RoundTripper
	hits    *atomic.Int32
	bodies  *[]*closeLog
	status  int
	header  http.Header
	payload string
}

func (s *scriptRT) Unwrap() http.RoundTripper { return s.inner }

func (s *scriptRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if s.hits != nil {
		s.hits.Add(1)
	}
	body := &closeLog{Reader: strings.NewReader(s.payload)}
	if s.bodies != nil {
		*s.bodies = append(*s.bodies, body)
	}
	hdr := s.header.Clone()
	if hdr == nil {
		hdr = make(http.Header)
	}
	return &http.Response{
		StatusCode:    s.status,
		Header:        hdr,
		Body:          body,
		Request:       r,
		ContentLength: int64(len(s.payload)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
	}, nil
}

func TestDoFreebuffHTTPClosesPeekedBodies(t *testing.T) {
	longApp := "invalid api key\n" + strings.Repeat("y", 4000)
	longProxy := "blocked-by-allowlist\n" + strings.Repeat("y", 4000)
	proxyURL, err := url.Parse("http://127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		proxy      bool
		payload    string
		wantDirect int32
	}{
		{name: "direct short application 403", payload: "invalid api key"},
		{name: "direct long application 403", payload: longApp},
		{name: "direct long proxy-shaped body", payload: longProxy},
		{name: "proxy short application 403", proxy: true, payload: "invalid api key"},
		{name: "proxy long application 403", proxy: true, payload: longApp},
		{name: "proxy short refusal", proxy: true, payload: "blocked-by-allowlist", wantDirect: 1},
		{name: "proxy long refusal", proxy: true, payload: longProxy, wantDirect: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var leaked atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				leaked.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer upstream.Close()

			var hits atomic.Int32
			var bodies []*closeLog
			base := &scriptRT{
				hits: &hits, bodies: &bodies,
				status: http.StatusForbidden, payload: tc.payload,
				header: http.Header{"Content-Type": []string{"text/plain"}},
			}
			if tc.proxy {
				base.inner = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, upstream.URL, strings.NewReader(`{"n":1}`))
			if err != nil {
				t.Fatal(err)
			}
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{"n":1}`)), nil
			}
			resp, err := DoFreebuffHTTP(context.Background(), &http.Client{Transport: base}, req)
			if err != nil {
				t.Fatal(err)
			}
			got, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if hits.Load() != 1 || leaked.Load() != tc.wantDirect {
				t.Fatalf("hits=%d direct=%d", hits.Load(), leaked.Load())
			}
			if len(bodies) != 1 || bodies[0].closes != 1 || readErr != nil || closeErr != nil {
				t.Fatalf("closes=%d read=%v close=%v", closesOf(bodies), readErr, closeErr)
			}
			if tc.wantDirect == 0 && string(got) != tc.payload {
				t.Fatalf("body len=%d, want %d", len(got), len(tc.payload))
			}
			if tc.wantDirect == 1 && !strings.Contains(string(got), "ok") {
				t.Fatalf("direct body=%q", got)
			}
		})
	}
}

func TestDoFreebuffHTTPDoesNotReplayAgainBehindFallbackTransport(t *testing.T) {
	var leaked atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxyURL, err := url.Parse("http://127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	var baseHits, directHits atomic.Int32
	var baseBodies []*closeLog
	base := &scriptRT{
		inner: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		hits:  &baseHits, bodies: &baseBodies,
		status: http.StatusForbidden, payload: "blocked-by-allowlist\n" + strings.Repeat("z", 4000),
		header: http.Header{"Content-Type": []string{"text/plain"}},
	}
	direct := &scriptRT{
		hits: &directHits, status: http.StatusOK, payload: `{"ok":true}`,
		header: http.Header{"Content-Type": []string{"application/json"}},
	}
	client := &http.Client{Transport: &proxy.FallbackTransport{Base: base, Direct: direct}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, upstream.URL, strings.NewReader(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"n":1}`)), nil
	}
	resp, err := DoFreebuffHTTP(context.Background(), client, req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if baseHits.Load() != 1 || directHits.Load() != 1 || leaked.Load() != 0 {
		t.Fatalf("base=%d direct=%d leaked=%d", baseHits.Load(), directHits.Load(), leaked.Load())
	}
	if len(baseBodies) != 1 || baseBodies[0].closes != 1 {
		t.Fatalf("base Close calls=%d, want 1", baseBodies[0].closes)
	}
}

func closesOf(bodies []*closeLog) int {
	if len(bodies) == 0 {
		return -1
	}
	return bodies[0].closes
}
