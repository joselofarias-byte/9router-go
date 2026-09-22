package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"9router/proxy/internal/providers"
)

// DirectLoopbackClient returns a client that dials only loopback addresses,
// ignores HTTP_PROXY/HTTPS_PROXY, and refuses redirects off the machine.
// base supplies Timeout and ResponseHeaderTimeout when it is an *http.Transport.
func DirectLoopbackClient(base *http.Client) *http.Client {
	var timeout time.Duration
	var headerTimeout time.Duration
	if base != nil {
		timeout = base.Timeout
		if tr, ok := base.Transport.(*http.Transport); ok && tr != nil {
			headerTimeout = tr.ResponseHeaderTimeout
		}
	}
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: refuseOffMachineRedirect,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialLoopback,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: headerTimeout,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func refuseOffMachineRedirect(req *http.Request, via []*http.Request) error {
	if req == nil || req.URL == nil || !providers.IsLoopbackURL(req.URL.String()) {
		host := ""
		if req != nil && req.URL != nil {
			host = req.URL.Hostname()
		}
		return fmt.Errorf("local upstream refused redirect to non-loopback host %q", host)
	}
	if len(via) >= 5 {
		return fmt.Errorf("local upstream stopped after 5 redirects")
	}
	return nil
}

func dialLoopback(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("local upstream address: %w", err)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return nil, fmt.Errorf("%w: %s", providers.ErrNotLoopback, ip)
		}
		return (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("local upstream resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: %s resolved to no addresses", providers.ErrNotLoopback, host)
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() {
			return nil, fmt.Errorf("%w: %s resolved to %s", providers.ErrNotLoopback, host, ip.IP)
		}
	}
	return (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(host, port))
}
