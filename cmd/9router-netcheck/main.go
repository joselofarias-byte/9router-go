// A credential-free DNS/TLS smoke check using the same build as the gateway.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	failed := false
	for _, host := range []string{"cli-chat-proxy.grok.com", "auth.x.ai"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		cancel()
		if err != nil {
			fmt.Printf("%s DNS ERROR: %v\n", host, err)
			failed = true
			continue
		}
		fmt.Printf("%s DNS OK (%d direcciones)\n", host, len(ips))
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS12}}
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
		cancel()
		if err != nil {
			fmt.Printf("%s TLS ERROR: %v\n", host, err)
			failed = true
			continue
		}
		conn.Close()
		fmt.Printf("%s TLS OK\n", host)
	}
	if failed {
		os.Exit(1)
	}
}
