package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/auth"
	"9router/proxy/internal/db"
)

// Grok OAuth credentials belong to the global provider pool, so a client
// inference key must never be allowed to import them, even with dashboard
// login disabled. All tokens below are synthetic; no xAI calls are made.
func TestSetupServerRouter_GrokBulkImportRequiresAdmin(t *testing.T) {
	t.Setenv("JWT_SECRET", "grok-bulk-import-test-secret")
	t.Setenv("DATA_DIR", t.TempDir())
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)

	if _, err := database.Exec(`INSERT INTO apiKeys (id, key, name, isActive, createdAt) VALUES ('grok-client', 'grok-engine-test-key', 'test inference client', 1, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed inference API key: %v", err)
	}

	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)
	const path = "/api/oauth/grok-cli/bulk-import"
	const payload = `{"accounts":[{"access_token":"synthetic-grok-access","refresh_token":"synthetic-grok-refresh","email":"grok-test@example.invalid"}]}`

	post := func(setAuth func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if setAuth != nil {
			setAuth(req)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	countConnections := func() int {
		t.Helper()
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM providerConnections WHERE provider = 'grok-cli'").Scan(&count); err != nil {
			t.Fatalf("count Grok connections: %v", err)
		}
		return count
	}

	engineAuth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer grok-engine-test-key")
	}
	customKeyAuth := func(req *http.Request) {
		req.Header.Set("X-API-Key", "grok-engine-test-key")
	}

	for _, tc := range []struct {
		name string
		auth func(*http.Request)
	}{
		{"anonymous", nil},
		{"inference Bearer key", engineAuth},
		{"inference X-API-Key", customKeyAuth},
		{"bogus local admin token", func(req *http.Request) {
			req.Header.Set(auth.CLITokenHeader, "invalid-grok-admin-token")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(tc.auth)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
			}
			if count := countConnections(); count != 0 {
				t.Fatalf("unauthorized call inserted %d connections", count)
			}
		})
	}

	// Even an intentionally open dashboard must not grant credential writes.
	if err := repo.UpdateSettingsRaw(map[string]any{"requireLogin": false}); err != nil {
		t.Fatalf("disable dashboard login for regression test: %v", err)
	}
	for _, tc := range []struct {
		name string
		auth func(*http.Request)
	}{
		{"open dashboard anonymous", nil},
		{"open dashboard inference key", engineAuth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(tc.auth)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 with open dashboard, got %d: %s", rec.Code, rec.Body.String())
			}
			if count := countConnections(); count != 0 {
				t.Fatalf("unauthorized call inserted %d connections", count)
			}
		})
	}

	// A real local administrative session can import synthetic credentials.
	session, err := auth.Sign(auth.Secret(), time.Now())
	if err != nil {
		t.Fatalf("sign admin session: %v", err)
	}
	sessionRec := post(func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	})
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("admin session import status = %d: %s", sessionRec.Code, sessionRec.Body.String())
	}
	if count := countConnections(); count != 1 {
		t.Fatalf("admin session should insert 1 connection, got %d", count)
	}

	// An authentic on-device CLI token remains an alternative admin credential.
	cliRec := post(func(req *http.Request) {
		req.Header.Set(auth.CLITokenHeader, auth.CLIToken())
	})
	if cliRec.Code != http.StatusOK {
		t.Fatalf("local CLI import status = %d: %s", cliRec.Code, cliRec.Body.String())
	}
	if count := countConnections(); count != 2 {
		t.Fatalf("CLI admin token should insert another connection, got %d", count)
	}
}

func TestSetupRoutes_GrokBulkImportNotMountedUnderInferenceKey(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	r := chi.NewRouter()
	SetupRoutes(r, db.NewRepo(database), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/oauth/grok-cli/bulk-import", strings.NewReader("{}")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected no Grok bulk import handler in inference routes, got %d: %s", rec.Code, rec.Body.String())
	}
}
