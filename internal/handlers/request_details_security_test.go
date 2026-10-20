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

func TestRequestDetailsRequireAdminNotInferenceKey(t *testing.T) {
    t.Setenv("JWT_SECRET", "grok004-security-test-secret")
    database, cleanup := setupTestDB(t)
    defer cleanup()
    repo := db.NewRepo(database)
    if _, err := database.Exec(`INSERT INTO apiKeys (id, key, name, isActive, createdAt)
        VALUES ('grok004-test', 'grok004-active-engine-key', 'engine', 1, '2026-01-01T00:00:00Z')`); err != nil {
        t.Fatalf("create inference key: %v", err)
    }
    if err := repo.InsertRequestDetail("grok004-detail", "test-provider", "test-model", "test-connection", "success", `{"request":{"messages":[{"content":"private-grok004-canary"}]}}`); err != nil {
        t.Fatalf("seed request detail: %v", err)
    }
    router := chi.NewRouter()
    SetupServerRouter(router, repo, nil)
    endpoint := "/api/usage/request-details"

    check := func(label string, req *http.Request, want int) string {
        t.Helper()
        rec := httptest.NewRecorder()
        router.ServeHTTP(rec, req)
        if rec.Code != want {
            t.Fatalf("%s status=%d want=%d body=%s", label, rec.Code, want, rec.Body.String())
        }
        return rec.Body.String()
    }

    check("anonymous", httptest.NewRequest(http.MethodGet, endpoint, nil), http.StatusUnauthorized)
    inference := httptest.NewRequest(http.MethodGet, endpoint, nil)
    inference.Header.Set("Authorization", "Bearer grok004-active-engine-key")
    body := check("inference key", inference, http.StatusUnauthorized)
    if strings.Contains(body, "private-grok004-canary") {
        t.Fatal("inference key exposed private diagnostic content")
    }

    if err := repo.UpdateSettingsRaw(map[string]any{"requireLogin": false}); err != nil {
        t.Fatalf("settings: %v", err)
    }
    check("open dashboard must not open diagnostics", httptest.NewRequest(http.MethodGet, endpoint, nil), http.StatusUnauthorized)

    token, err := auth.Sign("grok004-security-test-secret", time.Now())
    if err != nil {
        t.Fatalf("sign admin session: %v", err)
    }
    admin := httptest.NewRequest(http.MethodGet, endpoint, nil)
    admin.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
    body = check("admin session", admin, http.StatusOK)
    if !strings.Contains(body, "private-grok004-canary") {
        t.Fatal("admin request detail endpoint unavailable")
    }
}
