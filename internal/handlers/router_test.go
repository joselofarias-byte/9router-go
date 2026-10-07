package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/db"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	tmpFile, err := os.CreateTemp("", "test_router_*.sqlite")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	database, err := db.OpenDatabase(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("OpenDatabase failed: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.Remove(tmpFile.Name())
	}
	return database, cleanup
}

func TestSetupRoutes(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupRoutes(r, repo, nil)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusMethodNotAllowed || w.Code == http.StatusNotFound {
		t.Errorf("expected /chat/completions route to be registered, got status %d", w.Code)
	}
}


func TestSetupServerRouter_MountsAuthenticatedFabricAdminRoutes(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := database.Exec(`
		INSERT INTO apiKeys (id, key, name, machineId, isActive, createdAt)
		VALUES ('fabric-admin-test', 'fabric-admin-token', 'Fabric admin test', 'ci', 1, datetime('now'))
	`); err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	if err := registry.InitRegistry(database); err != nil {
		t.Fatalf("init registry: %v", err)
	}

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	unauth := httptest.NewRequest(http.MethodGet, "/api/admin/registry", nil)
	unauthRec := httptest.NewRecorder()
	r.ServeHTTP(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated admin registry to return 401, got %d", unauthRec.Code)
	}

	for _, path := range []string{
		"/api/admin/registry",
		"/api/admin/explain-route?model=gpt-4&policy=free-first",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer fabric-admin-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected authenticated %s to return 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}
