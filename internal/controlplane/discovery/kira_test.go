package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A trimmed shape of the real https://kiraai.vn/v1/models/ response (verified
// live on 2026-09-11): paid chat models, free chat models, and non-chat
// (image) free models must be filtered down to just the free chat set.
const kiraMockCatalog = `{
	"object": "list",
	"data": [
		{"id": "kira-3.5-pro", "object": "model", "type": "chat", "status": "active", "is_free": false},
		{"id": "glm-5.3-flash-free", "object": "model", "type": "chat", "status": "active", "is_free": true},
		{"id": "qwen3.8-flash-free", "object": "model", "type": "chat", "status": "active", "is_free": true},
		{"id": "kira-2.0-image", "object": "model", "type": "image", "status": "active", "is_free": true},
		{"id": "kira-mini-1.0", "object": "model", "type": "chat", "status": "inactive", "is_free": true}
	]
}`

func TestKiraAdapter_Discover(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("catalog discovery must not require an Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(kiraMockCatalog))
	}))
	defer ts.Close()

	adapter := NewKiraAdapter(ts.Client(), ts.URL+"/v1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	candidates, err := adapter.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{"glm-5.3-flash-free": true, "qwen3.8-flash-free": true}
	if len(candidates) != len(want) {
		t.Fatalf("expected %d free chat candidates, got %d: %+v", len(want), len(candidates), candidates)
	}
	for _, c := range candidates {
		if !want[c.ModelID] {
			t.Errorf("unexpected candidate model %q (paid, non-chat, or inactive models must be filtered out)", c.ModelID)
		}
		if c.ProviderID != "kira" {
			t.Errorf("expected provider id 'kira', got %q", c.ProviderID)
		}
		if c.PricingMode != "free_tier" {
			t.Errorf("expected pricing mode 'free_tier', got %q", c.PricingMode)
		}
	}
}

func TestKiraAdapter_DiscoverAuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	adapter := NewKiraAdapter(ts.Client(), ts.URL+"/v1")
	if _, err := adapter.Discover(context.Background()); err == nil {
		t.Fatal("expected an error on 401 from the catalog endpoint")
	}
}

func TestKiraAdapter_DefaultBaseURL(t *testing.T) {
	adapter := NewKiraAdapter(nil, "")
	if adapter.baseURL != "https://kiraai.vn/v1" {
		t.Errorf("expected default base URL, got %q", adapter.baseURL)
	}
}
