package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnoRouterAdapter_Discover(t *testing.T) {
	mockResponse := `{
		"data": [
			{"id": "gpt-4:free"},
			{"id": "claude-3-opus"}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	origURL := UnoRouterCatalogURL
	defer func() { UnoRouterCatalogURL = origURL }()
	UnoRouterCatalogURL = server.URL

	adapter := NewUnoRouterAdapter(server.Client())
	candidates, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	c1 := candidates[0]
	if c1.ModelID != "gpt-4:free" || c1.PricingMode != "free" {
		t.Errorf("unexpected free candidate: %+v", c1)
	}

	c2 := candidates[1]
	if c2.ModelID != "claude-3-opus" || c2.PricingMode != "unknown" {
		t.Errorf("unexpected paid candidate: %+v", c2)
	}
}
