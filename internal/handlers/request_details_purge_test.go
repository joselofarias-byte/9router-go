package handlers

import (
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/go-chi/chi/v5"
    "9router/proxy/internal/auth"
    "9router/proxy/internal/db"
)

func TestGROK004RequestDetailsPurgeIsAdminOnly(t *testing.T) {
    t.Setenv("JWT_SECRET", "grok004-purge-authorization-secret")
    database, cleanup := setupTestDB(t)
    defer cleanup()
    repo := db.NewRepo(database)
    if err := repo.InsertRequestDetail("purge-1", "provider", "model", "connection", "success", `{"id":"purge-1"}`); err != nil {
        t.Fatal(err)
    }
    if _, err := database.Exec(`INSERT INTO apiKeys(id, key, name, isActive, createdAt)
        VALUES('purge-inference', 'purge-inference-key', 'inference', 1, '2026-01-01T00:00:00Z')`); err != nil {
        t.Fatal(err)
    }
    router := chi.NewRouter()
    SetupServerRouter(router, repo, nil)
    path := "/api/usage/request-details"
    execute := func(request *http.Request, want int) {
        t.Helper()
        rec := httptest.NewRecorder()
        router.ServeHTTP(rec, request)
        if rec.Code != want {
            t.Fatalf("expected HTTP %d, got %d", want, rec.Code)
        }
    }
    anonymous := httptest.NewRequest(http.MethodDelete, path, nil)
    anonymous.Header.Set("X-Capimux-Confirm", "purge-request-details")
    execute(anonymous, http.StatusUnauthorized)
    inference := httptest.NewRequest(http.MethodDelete, path, nil)
    inference.Header.Set("Authorization", "Bearer purge-inference-key")
    inference.Header.Set("X-Capimux-Confirm", "purge-request-details")
    execute(inference, http.StatusUnauthorized)
    token, err := auth.Sign("grok004-purge-authorization-secret", time.Now())
    if err != nil { t.Fatal(err) }
    noConfirmation := httptest.NewRequest(http.MethodDelete, path, nil)
    noConfirmation.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
    execute(noConfirmation, http.StatusPreconditionRequired)
    confirmed := httptest.NewRequest(http.MethodDelete, path, nil)
    confirmed.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
    confirmed.Header.Set("X-Capimux-Confirm", "purge-request-details")
    execute(confirmed, http.StatusOK)
    _, count, err := repo.GetRequestDetailsPaged(10, 0)
    if err != nil || count != 0 {
        t.Fatalf("purge did not clear request details: count=%d err=%v", count, err)
    }
}
