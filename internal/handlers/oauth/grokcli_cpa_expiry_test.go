package oauth

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"9router/proxy/internal/db"
)

// CPA xAI credentials from AaronL725/grok-register carry both an absolute
// "expired" timestamp and "expires_in", which is relative to last_refresh,
// not to the instant the archive is later imported into 9router.
func TestGrokCLIImportItem_PreservesCPAExpiry(t *testing.T) {
	cases := []struct {
		name string
		item GrokCliImportItem
		want string
	}{
		{
			name: "CPA absolute expired takes priority",
			item: GrokCliImportItem{Expired: "2026-01-01T06:00:00Z", ExpiresIn: 21600, LastRefresh: "2026-01-01T00:00:00Z"},
			want: "2026-01-01T06:00:00Z",
		},
		{
			name: "explicit snake case retains compatibility",
			item: GrokCliImportItem{ExpiresAt: "2026-01-02T02:00:00Z", Expired: "2026-01-01T06:00:00Z"},
			want: "2026-01-02T02:00:00Z",
		},
		{
			name: "explicit camel case retains compatibility",
			item: GrokCliImportItem{ExpiresAtCamel: "2026-01-03T03:00:00Z", Expired: "2026-01-01T06:00:00Z"},
			want: "2026-01-03T03:00:00Z",
		},
		{
			name: "fallback relative expiry uses original refresh timestamp",
			item: GrokCliImportItem{ExpiresIn: 7200, LastRefresh: "2026-01-01T00:00:00Z"},
			want: "2026-01-01T02:00:00Z",
		},
		{
			name: "camel relative expiry uses original refresh timestamp",
			item: GrokCliImportItem{ExpiresInCamel: 3600, LastRefresh: "2026-01-01T00:00:00Z"},
			want: "2026-01-01T01:00:00Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.GetExpiresAt(); got != tc.want {
				t.Fatalf("GetExpiresAt() = %q, want %q", got, tc.want)
			}
		})
	}

	// Existing payloads without timestamp anchors are interpreted as fresh
	// access-token responses and continue to work as before.
	before := time.Now().UTC()
	fresh := (&GrokCliImportItem{ExpiresIn: 60}).GetExpiresAt()
	got, err := time.Parse(time.RFC3339, fresh)
	if err != nil {
		t.Fatalf("parse fresh expiry %q: %v", fresh, err)
	}
	if got.Before(before.Add(59*time.Second)) || got.After(time.Now().Add(61*time.Second)) {
		t.Fatalf("fresh relative expiry outside expected window: %v", got)
	}
}

func TestHandleOAuthGrokCliBulkImport_CPAXAIExport(t *testing.T) {
	database, cleanup := setupOAuthTestDB(t)
	defer cleanup()
	handler := NewOAuthHandler(db.NewRepo(database))

	// Synthetic CPA xAI JSON matching the upstream writer; no real secrets,
	// external APIs, or account generation.
	body := `{
		"type": "xai",
		"email": "fixture@example.invalid",
		"access_token": "synthetic-access",
		"refresh_token": "synthetic-refresh",
		"expires_in": 21600,
		"last_refresh": "2026-01-01T00:00:00Z",
		"expired": "2026-01-01T06:00:00Z",
		"base_url": "https://cli-chat-proxy.grok.com/v1",
		"token_endpoint": "https://auth.x.ai/oauth2/token"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/grok-cli/bulk-import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.HandleOAuthGrokCliBulkImport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("CPA import status = %d: %s", rec.Code, rec.Body.String())
	}

	var savedJSON string
	if err := database.QueryRow(`SELECT data FROM providerConnections WHERE provider='grok-cli' LIMIT 1`).Scan(&savedJSON); err != nil {
		t.Fatalf("read imported Grok connection: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal([]byte(savedJSON), &saved); err != nil {
		t.Fatalf("decode saved connection: %v", err)
	}
	if got := saved["expiresAt"]; got != "2026-01-01T06:00:00Z" {
		t.Fatalf("stored expiry = %v; should preserve CPA absolute date", got)
	}
	if got := saved["refreshToken"]; got != "synthetic-refresh" {
		t.Fatalf("stored refresh token = %v", got)
	}
}
