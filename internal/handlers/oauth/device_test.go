package oauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/db"
)

func TestHandleDeviceStart_qoderLocal(t *testing.T) {
	// Qoder start is fully local (no network): PKCE + nonce + URLs.
	handler := NewOAuthHandler(nil)
	req := httptest.NewRequest("POST", "/api/oauth/device/start", strings.NewReader(`{"provider":"qoder"}`))
	rec := httptest.NewRecorder()
	handler.HandleDeviceStart(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{
		`"device_code":`, `"user_code":`, `qoder.com/device/selectAccounts`,
		`"interval":2`, `"session":`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %q: %s", want, rec.Body.String())
		}
	}
}

func TestHandleDeviceStart_bad(t *testing.T) {
	handler := NewOAuthHandler(nil)
	for _, payload := range []string{`{"provider":"google"}`, `{"provider":"kiro","region":"evil;id"}`} {
		req := httptest.NewRequest("POST", "/api/oauth/device/start", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.HandleDeviceStart(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("expected error for %s, got %s", payload, rec.Body.String())
		}
	}
}

func TestHandleDevicePoll_validation(t *testing.T) {
	handler := NewOAuthHandler(nil)
	for _, payload := range []string{`{}`, `{"provider":"qoder"}`, `{"provider":"nope","device_code":"x"}`} {
		req := httptest.NewRequest("POST", "/api/oauth/device/poll", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.HandleDevicePoll(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %s, got %d", payload, rec.Code)
		}
	}
}

func TestKimiHeaders_deviceId(t *testing.T) {
	h := kimiHeaders("dev-1")
	if h["X-Msh-Device-Id"] != "dev-1" || h["X-Msh-Platform"] != "9router" {
		t.Errorf("bad kimi headers: %v", h)
	}
}

func TestHandleDevicePoll_PendingDoesNotInsert(t *testing.T) {
	original := deviceClient
	deviceClient = &http.Client{Transport: devicePollFixture(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"authorization_pending"}`)), Request: r}, nil
	})}
	t.Cleanup(func() { deviceClient = original })
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	handler := NewOAuthHandler(repo)

	// Qoder poll with nonexistent/unauthorized device code returns pending
	req := httptest.NewRequest("POST", "/api/oauth/device/poll", strings.NewReader(`{"provider":"qoder","device_code":"pending-code","session":{"verifier":"v"}}`))
	rec := httptest.NewRecorder()
	handler.HandleDevicePoll(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), `"status":"pending"`) && !strings.Contains(rec.Body.String(), `"status":"error"`) {
		t.Errorf("expected pending or error status, got: %s", rec.Body.String())
	}

	var count int
	err := repo.RawDB().QueryRow("SELECT COUNT(*) FROM providerConnections").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query providerConnections: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 connections inserted while pending, got %d", count)
	}
}

type devicePollFixture func(*http.Request) (*http.Response, error)

func (f devicePollFixture) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }


func TestGrokCLIProxyHeadersCurrentAndOverride(t *testing.T) {
	t.Setenv("GROK_CLI_CLIENT_VERSION", "1.2.3-test")
	h := grokcliProxyHeaders("token-value")
	if h["x-grok-client-version"] != "1.2.3-test" {
		t.Fatalf("client version = %q", h["x-grok-client-version"])
	}
	if h["X-XAI-Token-Auth"] != "xai-grok-cli" {
		t.Fatalf("token auth header = %q", h["X-XAI-Token-Auth"])
	}
	if h["x-authenticateresponse"] != "authenticate-response" {
		t.Fatalf("authenticate response header = %q", h["x-authenticateresponse"])
	}
	if h["x-grok-client-mode"] != "headless" {
		t.Fatalf("client mode = %q", h["x-grok-client-mode"])
	}
	if h["Authorization"] != "Bearer token-value" {
		t.Fatalf("authorization = %q", h["Authorization"])
	}
	if !strings.HasPrefix(h["User-Agent"], "grok-shell/1.2.3-test (") {
		t.Fatalf("user agent = %q", h["User-Agent"])
	}
}
