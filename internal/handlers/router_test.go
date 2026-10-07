package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"9router/proxy/internal/controlplane/registry"

	"github.com/go-chi/chi/v5"

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


func TestSetupServerRouter_AdminFabricRoutesAreMountedAndProtected(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS apiKeys (
			id TEXT PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			name TEXT,
			machineId TEXT,
			isActive INTEGER DEFAULT 1,
			createdAt TEXT NOT NULL
		);
		INSERT INTO apiKeys (id, key, name, machineId, isActive, createdAt)
		VALUES ('admin-test', 'admin-test-key', 'Admin Test', 'test-machine', 1, '2026-10-07T00:00:00Z');
	`); err != nil {
		t.Fatalf("create/seed API key: %v", err)
	}

	registry.InitRegistry(nil)
	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	for _, path := range []string{
		"/api/admin/registry",
		"/api/admin/explain-route?model=test-model&policy=balanced",
	} {
		t.Run(path+"_requires_auth", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthenticated admin route to return 401, got %d body=%s", w.Code, w.Body.String())
			}
		})

		t.Run(path+"_mounted", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer admin-test-key")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code == http.StatusNotFound {
				t.Fatalf("admin route is not mounted: %s", path)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("expected authenticated admin route to return 200, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
