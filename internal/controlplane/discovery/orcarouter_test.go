package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOrcaRouterAdapter_Discover(t *testing.T) {
	mockResponse := `{
		"data": [
			{
				"id": "glm-5.3-flash",
				"object": "model",
				"pricing": {
					"mode": "free_tier"
				}
			},
			{
				"id": "qwen3.8-27b",
				"object": "model",
				"cost": {
					"prompt": 0.0,
					"completion": 0.0
				}
			},
			{
				"id": "deepseek-coder",
				"object": "model",
				"pricing": {
					"mode": "paid"
				}
			},
			{
				"id": "some-free-model",
				"object": "model"
			}
		]
	}`

	var authHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models/free" && r.URL.Path != "/v1/public/models" && r.URL.Path != "/v1/models" {
			t.Errorf("Unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer ts.Close()

	client := ts.Client()
	adapter := NewOrcaRouterAdapter(client, ts.URL+"/v1", "test-api-key")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	candidates, err := adapter.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if authHeader != "Bearer test-api-key" {
		t.Errorf("expected Auth Header 'Bearer test-api-key', got '%s'", authHeader)
	}

	if len(candidates) != 4 {
		t.Fatalf("expected 4 candidates, got %d", len(candidates))
	}

	for _, c := range candidates {
		if c.ProviderID != "orcarouter" {
			t.Errorf("expected provider orcarouter, got %s", c.ProviderID)
		}

		if c.ModelID == "glm-5.3-flash" && c.PricingMode != "free_tier" {
			t.Errorf("expected free_tier pricing for glm-5.3-flash, got %s", c.PricingMode)
		}

		if c.ModelID == "qwen3.8-27b" && c.PricingMode != "free_tier" {
			t.Errorf("expected free_tier pricing (inferred from 0 cost) for qwen3.8-27b, got %s", c.PricingMode)
		}

		if c.ModelID == "deepseek-coder" && c.PricingMode != "paid" {
			t.Errorf("expected paid pricing for deepseek-coder, got %s", c.PricingMode)
		}

		if c.ModelID == "some-free-model" && c.PricingMode != "free_tier" {
			t.Errorf("expected free_tier pricing (inferred from string match) for some-free-model, got %s", c.PricingMode)
		}
	}
}

func TestOrcaRouterAdapter_AuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := ts.Client()
	adapter := NewOrcaRouterAdapter(client, ts.URL+"/v1", "bad-key")

	_, err := adapter.Discover(context.Background())
	if err == nil || err.Error() != "orcarouter authentication failed: 401" {
		t.Fatalf("expected auth failure error, got: %v", err)
	}
}

func TestOrcaRouterAdapter_RateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	client := ts.Client()
	adapter := NewOrcaRouterAdapter(client, ts.URL+"/v1", "key")

	_, err := adapter.Discover(context.Background())
	if err == nil || err.Error() != "orcarouter rate limited during discovery: 429" {
		t.Fatalf("expected rate limit error, got: %v", err)
	}
}
