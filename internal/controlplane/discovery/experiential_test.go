package discovery

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func experientialTestDB(t *testing.T, data string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE providerConnections (id TEXT PRIMARY KEY, provider TEXT NOT NULL, isActive INTEGER NOT NULL, data TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if data != "" {
		if _, err := db.Exec("INSERT INTO providerConnections (id, provider, isActive, data) VALUES (?, ?, 1, ?)", "exp-test", "experiential", data); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestExperientialFreeAdapterDiscoversOnlyStrictFreeAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xpl_test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"data\":[{\"id\":\"anthropic/claude-haiku-5.5:free\",\"canonical_slug\":\"claude-haiku-5.5\",\"name\":\"Claude Haiku 5.5\"},{\"id\":\"openai/gpt-6-luna-decisions:free\",\"canonical_slug\":\"gpt-6-luna-decisions\",\"name\":\"GPT-6 Luna Decisions\"},{\"id\":\"openai/gpt-6-sol\",\"canonical_slug\":\"gpt-6-sol\",\"name\":\"GPT-6 Sol\"},{\"id\":\"type-safe/jev-latest:free\",\"canonical_slug\":\"jev-latest\",\"name\":\"Jev\"},{\"id\":\"anthropic/claude-haiku-5.5:free\",\"canonical_slug\":\"claude-haiku-5.5\",\"name\":\"duplicate\"}]}"))
	}))
	defer server.Close()

	oldURL := ExperientialModelsURL
	ExperientialModelsURL = server.URL
	defer func() { ExperientialModelsURL = oldURL }()

	db := experientialTestDB(t, "{\"apiKey\":\"xpl_test\"}")
	got, err := NewExperientialFreeAdapter(db, server.Client()).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 strict-free candidates, got %d: %+v", len(got), got)
	}

	want := map[string]bool{
		"anthropic/claude-haiku-5.5:free": true,
		"openai/gpt-6-luna-decisions:free": true,
	}
	for _, candidate := range got {
		if candidate.ProviderID != "experiential" {
			t.Errorf("provider = %q", candidate.ProviderID)
		}
		if candidate.PricingMode != "free" {
			t.Errorf("pricing = %q", candidate.PricingMode)
		}
		if candidate.UpstreamModel != candidate.ModelID {
			t.Errorf("strict-free upstream id changed: %q vs %q", candidate.UpstreamModel, candidate.ModelID)
		}
		if !want[candidate.UpstreamModel] {
			t.Errorf("unexpected candidate %q", candidate.UpstreamModel)
		}
	}
}

func TestExperientialFreeAdapterWithoutCredentialIsEmpty(t *testing.T) {
	db := experientialTestDB(t, "")
	got, err := NewExperientialFreeAdapter(db, nil).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no candidates, got %+v", got)
	}
}

func TestExperientialFreeAdapterAuthFailureFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	oldURL := ExperientialModelsURL
	ExperientialModelsURL = server.URL
	defer func() { ExperientialModelsURL = oldURL }()
	db := experientialTestDB(t, "{\"apiKey\":\"xpl_revoked\"}")
	got, err := NewExperientialFreeAdapter(db, server.Client()).Discover(context.Background())
	if err != nil {
		t.Fatalf("auth rejection should fail closed, got error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty catalog after auth rejection, got %+v", got)
	}
}

func TestExperientialFreeAdapterTransientFailurePreservesSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	oldURL := ExperientialModelsURL
	ExperientialModelsURL = server.URL
	defer func() { ExperientialModelsURL = oldURL }()
	db := experientialTestDB(t, "{\"apiKey\":\"xpl_test\"}")
	_, err := NewExperientialFreeAdapter(db, server.Client()).Discover(context.Background())
	if err == nil {
		t.Fatal("expected transient catalog error")
	}
}

func TestExperientialFreeChatCompatibleRejectsJev(t *testing.T) {
	if experientialFreeChatCompatible(experientialCatalogModel{
		ID: "type-safe/jev-latest:free", CanonicalSlug: "jev-latest",
	}) {
		t.Fatal("Jev is native /v1/systemone only and must stay out of free-best")
	}
	if !experientialFreeChatCompatible(experientialCatalogModel{
		ID: "openai/gpt-6-luna-decisions:free", CanonicalSlug: "gpt-6-luna-decisions",
	}) {
		t.Fatal("chat-capable Experiential free model was rejected")
	}
}


func TestExperientialFreeChatCompatibleRejectsLunaDecisions(t *testing.T) {
	if experientialFreeChatCompatible(experientialCatalogModel{
		ID: "openai/gpt-6-luna-decisions:free", CanonicalSlug: "gpt-6-luna-decisions",
	}) {
		t.Fatal("GPT-6 Luna Decisions is a Decisions API model and must stay out of chat-only free-best")
	}
	if !experientialFreeChatCompatible(experientialCatalogModel{
		ID: "anthropic/claude-haiku-5.5:free", CanonicalSlug: "claude-haiku-5.5",
	}) {
		t.Fatal("chat-capable Experiential free model was rejected")
	}
}
