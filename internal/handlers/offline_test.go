package handlers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
)

// Keep ordinary tests offline even if a future NoAuth path ignores a fixture
// connection. Live runs must be selected deliberately.
func TestMain(m *testing.M) {
	if os.Getenv("NINEROUTER_LIVE_E2E") != "1" {
		transport := http.DefaultTransport.(*http.Transport)
		dial := transport.DialContext
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ip := net.ParseIP(host)
			if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
				return nil, fmt.Errorf("offline test refused external dial to %s", host)
			}
			return dial(ctx, network, address)
		}
		http.DefaultTransport = transport
	}
	os.Exit(m.Run())
}
