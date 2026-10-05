package entitlements

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTransportActivationContract(t *testing.T) {
	serverTime := time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/activate" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var request ActivationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ActivationCode != "test-activation-code" {
			t.Fatalf("activation code = %q", request.ActivationCode)
		}
		if request.InstallationID != "install-test" {
			t.Fatalf("installation id = %q", request.InstallationID)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"lease\":{\"version\":1},\"server_time\":%q,\"renewal_after_seconds\":90}", serverTime.Format(time.RFC3339))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.Activate(context.Background(), ActivationRequest{
		ActivationCode: "test-activation-code",
		InstallationID: "install-test",
		Platform:       "android",
		Arch:           "arm64",
		BuildChannel:   "beta",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Lease) != "{"version":1}" {
		t.Fatalf("lease = %s", response.Lease)
	}
	if !response.ServerTime.Equal(serverTime) {
		t.Fatalf("server time = %s", response.ServerTime)
	}
	if response.RenewalAfterSeconds != 90 {
		t.Fatalf("renewal after = %d", response.RenewalAfterSeconds)
	}
}

func TestHTTPTransportRenewalPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/lease/renew" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var request RenewalRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.LicenseID != "lic-http-1" || request.CurrentNonce != "nonce-http-1" {
			t.Fatalf("renew request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"lease":{"version":1}}"))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Renew(context.Background(), RenewalRequest{
		LicenseID:      "lic-http-1",
		InstallationID: "install-http-1",
		CurrentNonce:   "nonce-http-1",
		Platform:       "linux",
		Arch:           "amd64",
		BuildChannel:   "beta",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTransportSanitizesNon2xxErrors(t *testing.T) {
	const secret = "activation-code-must-not-appear"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid activation code: "+secret, http.StatusUnauthorized)
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Activate(context.Background(), ActivationRequest{
		ActivationCode: secret,
		InstallationID: "install-test",
		Platform:       "android",
		Arch:           "arm64",
		BuildChannel:   "beta",
	})
	if err == nil {
		t.Fatal("non-2xx response unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked activation code: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPTransportBoundsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 128)))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	transport.maxResponseBytes = 32

	_, err = transport.Renew(context.Background(), RenewalRequest{
		LicenseID:      "lic",
		InstallationID: "install",
		CurrentNonce:   "nonce",
		Platform:       "linux",
		Arch:           "amd64",
		BuildChannel:   "beta",
	})
	if !errors.Is(err, ErrControlPlaneResponseTooLarge) {
		t.Fatalf("error = %v, want %v", err, ErrControlPlaneResponseTooLarge)
	}
}
