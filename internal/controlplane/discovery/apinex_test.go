package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPInexAdapterDiscoversOnlyFreeModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-apx-test" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"free/gpt-5.6-luna","object":"model"},
			{"id":"free/kimi-k3","object":"model"},
			{"id":"gpt-6-astra","object":"model"},
			{"id":"free/kimi-k3","object":"model"},
			{"id":"","object":"model"},
			{"id":"free/not-a-model","object":"other"}
		]}`))
	}))
	defer srv.Close()

	got, err := NewAPInexAdapter(srv.Client(), srv.URL, "sk-apx-test").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique free candidates, got %d: %+v", len(got), got)
	}
	if got[0].ModelID != "free/gpt-5.6-luna" || got[1].ModelID != "free/kimi-k3" {
		t.Fatalf("unexpected models: %+v", got)
	}
	for _, candidate := range got {
		if candidate.ProviderID != "apinex" {
			t.Errorf("provider = %q, want apinex", candidate.ProviderID)
		}
		if candidate.PricingMode != "free" {
			t.Errorf("pricing = %q, want free", candidate.PricingMode)
		}
		if candidate.UpstreamModel != candidate.ModelID {
			t.Errorf("upstream model = %q, model = %q", candidate.UpstreamModel, candidate.ModelID)
		}
	}
}

func TestAPInexAdapterRejectsAuthAndRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			if _, err := NewAPInexAdapter(srv.Client(), srv.URL, "bad-key").Discover(context.Background()); err == nil {
				t.Fatalf("expected error for HTTP %d", status)
			}
		})
	}
}
