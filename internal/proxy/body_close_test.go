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

// closeLog is an HTTP body that records Close so a wrapper cannot hide it.
type closeLog struct {
	io.Reader
	closes int
}

func (c *closeLog) Close() error {
	c.closes++
	return nil
}

func TestProxyRefusalPeekClosesOriginalBody(t *testing.T) {
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
			if got := isProxyFailure(nil, resp); got != tc.refuse {
				t.Fatalf("isProxyFailure=%v, want %v", got, tc.refuse)
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

// scriptRT is a RoundTripper whose body records Close. Unwrap exposes a real
// *http.Transport so requestUsesProxy can see a proxy without dialing it.
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

func proxyTransport() *http.Transport {
	proxyURL, _ := url.Parse("http://127.0.0.1:9")
	return &http.Transport{Proxy: http.ProxyURL(proxyURL)}
}

func TestDoRequestClosesPeekedBodies(t *testing.T) {
	longApp := "invalid api key\n" + strings.Repeat("y", 4000)
	longProxy := "blocked-by-allowlist\n" + strings.Repeat("y", 4000)
	cases := []struct {
		name       string
		proxy      bool
		payload    string
		wantDirect int32
		wantErr    bool
	}{
		{name: "direct short application 403", payload: "invalid api key", wantErr: true},
		{name: "direct long application 403", payload: longApp, wantErr: true},
		{name: "direct long proxy-shaped body", payload: longProxy, wantErr: true},
		{name: "proxy short application 403", proxy: true, payload: "invalid api key", wantErr: true},
		{name: "proxy long application 403", proxy: true, payload: longApp, wantErr: true},
		{name: "proxy short refusal", proxy: true, payload: "blocked-by-allowlist", wantDirect: 1},
		{name: "proxy long refusal", proxy: true, payload: longProxy, wantDirect: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var leaked atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				leaked.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"direct"}}]}`))
			}))
			defer upstream.Close()

			var proxyHits atomic.Int32
			var bodies []*closeLog
			base := &scriptRT{
				hits: &proxyHits, bodies: &bodies,
				status: http.StatusForbidden, payload: tc.payload,
				header: http.Header{"Content-Type": []string{"text/plain"}},
			}
			if tc.proxy {
				base.inner = proxyTransport()
			}
			client := &http.Client{Transport: base}
			resp, err := DoRequest(context.Background(), client, http.MethodPost, upstream.URL, nil, []byte(`{"n":1}`))
			if proxyHits.Load() != 1 {
				t.Fatalf("transport hits=%d, want 1", proxyHits.Load())
			}
			if leaked.Load() != tc.wantDirect {
				t.Fatalf("direct dials=%d, want %d", leaked.Load(), tc.wantDirect)
			}
			if len(bodies) != 1 || bodies[0].closes != 1 {
				t.Fatalf("original closes=%v, want 1 call", closesOf(bodies))
			}
			if tc.wantErr {
				var ue *UpstreamError
				if !errors.As(err, &ue) || ue.StatusCode != http.StatusForbidden || string(ue.Body) != tc.payload {
					t.Fatalf("err=%v bodyLen=%d", err, errorBodyLen(err))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			got, readErr := io.ReadAll(resp.Body)
			if readErr != nil || !strings.Contains(string(got), "direct") {
				t.Fatalf("direct body=%q err=%v", got, readErr)
			}
		})
	}
}

func TestDoRequestDoesNotReplayAgainBehindFallbackTransport(t *testing.T) {
	longProxy := "blocked-by-allowlist\n" + strings.Repeat("z", 4000)
	cases := []struct {
		name        string
		basePayload string
		baseStatus  int
		baseType    string
		directBody  string
		directCode  int
		wantDirect  int32
		wantErr     bool
	}{
		{name: "proxy refusal then direct 200", basePayload: "blocked-by-allowlist", baseStatus: http.StatusForbidden, baseType: "text/plain", directBody: `{"ok":true}`, directCode: http.StatusOK, wantDirect: 1},
		{name: "long proxy refusal then direct 200", basePayload: longProxy, baseStatus: http.StatusForbidden, baseType: "text/plain", directBody: `{"ok":true}`, directCode: http.StatusOK, wantDirect: 1},
		{name: "proxy refusal then direct refusal is one direct hop", basePayload: "blocked-by-allowlist", baseStatus: http.StatusForbidden, baseType: "text/plain", directBody: "blocked-by-allowlist", directCode: http.StatusForbidden, wantDirect: 1, wantErr: true},
		{name: "application 403 is not replayed by either layer", basePayload: "invalid api key", baseStatus: http.StatusForbidden, baseType: "text/plain", directBody: `{"ok":true}`, directCode: http.StatusOK, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var leaked atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				leaked.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			var baseHits, directHits atomic.Int32
			var baseBodies []*closeLog
			base := &scriptRT{
				inner: proxyTransport(), hits: &baseHits, bodies: &baseBodies,
				status: tc.baseStatus, payload: tc.basePayload,
				header: http.Header{"Content-Type": []string{tc.baseType}},
			}
			direct := &scriptRT{
				hits: &directHits, status: tc.directCode, payload: tc.directBody,
				header: http.Header{"Content-Type": []string{"text/plain"}},
			}
			client := &http.Client{Transport: &FallbackTransport{Base: base, Direct: direct}}
			resp, err := DoRequest(context.Background(), client, http.MethodPost, upstream.URL, nil, []byte(`{"n":1}`))
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if baseHits.Load() != 1 || directHits.Load() != tc.wantDirect || leaked.Load() != 0 {
				t.Fatalf("base=%d direct=%d leaked=%d err=%v", baseHits.Load(), directHits.Load(), leaked.Load(), err)
			}
			if len(baseBodies) != 1 || baseBodies[0].closes != 1 {
				t.Fatalf("base Close calls=%v, want 1", closesOf(baseBodies))
			}
			if tc.wantErr {
				var ue *UpstreamError
				if !errors.As(err, &ue) || ue.StatusCode != tc.baseStatus {
					t.Fatalf("err=%v", err)
				}
				if tc.wantDirect == 0 && string(ue.Body) != tc.basePayload {
					t.Fatalf("error body len=%d, want %d", len(ue.Body), len(tc.basePayload))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func closesOf(bodies []*closeLog) []int {
	out := make([]int, len(bodies))
	for i, body := range bodies {
		out[i] = body.closes
	}
	return out
}

func errorBodyLen(err error) int {
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return len(ue.Body)
	}
	return -1
}
