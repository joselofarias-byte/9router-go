package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsDevAdapter_Discover(t *testing.T) {
	mockResponse := `{
		"openai": {
			"models": {
				"gpt-4": {
					"modality": {"text": true, "vision": true},
					"limit": {"context": 8192, "output": 4096}
				}
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	// temporarily swap the global URL for testing
	origURL := ModelsDevCatalogURL
	defer func() { ModelsDevCatalogURL = origURL }()

	// Test 1: Successful parsing
	ModelsDevCatalogURL = server.URL
	adapter := NewModelsDevAdapter(server.Client())
	candidates, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	c := candidates[0]
	if c.ProviderID != "openai" || c.ModelID != "gpt-4" {
		t.Errorf("unexpected candidate identity: %s/%s", c.ProviderID, c.ModelID)
	}

	// Test 2: HTTP Error handling
	ModelsDevCatalogURL = server.URL + "/error"
	_, err = adapter.Discover(context.Background())
	if err == nil {
		t.Errorf("expected error on 500 response, got nil")
	}
}
