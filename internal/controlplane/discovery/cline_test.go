package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClineFreeAdapterDiscoversOnlyFreeModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"recommended":[{"id":"openai/gpt-6-astra","name":"gpt-6-astra"}],
			"free":[
				{"id":"cline-free/muse-spark-1.3-contributor","name":"Muse Spark 1.3 Contributor","description":"free","tags":[]},
				{"id":"deepseek/deepseek-v4-flash","name":"deepseek-v4-flash","description":"free","tags":[]},
				{"id":"deepseek/deepseek-v4-flash","name":"duplicate","description":"duplicate","tags":[]}
			],
			"clinePass":[{"id":"cline-pass/deepseek-v4-pro","name":"paid"}]
		}`))
	}))
	defer server.Close()

	oldURL := ClineRecommendedModelsURL
	ClineRecommendedModelsURL = server.URL
	defer func() { ClineRecommendedModelsURL = oldURL }()

	adapter := NewClineFreeAdapter(server.Client())
	got, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 unique free candidates, got %d: %+v", len(got), got)
	}
	for _, candidate := range got {
		if candidate.ProviderID != "cline" {
			t.Errorf("expected provider cline, got %q", candidate.ProviderID)
		}
		if candidate.PricingMode != "free" {
			t.Errorf("expected free pricing, got %q", candidate.PricingMode)
		}
		if candidate.UpstreamModel != candidate.ModelID {
			t.Errorf("expected exact upstream model ID, got %q vs %q", candidate.UpstreamModel, candidate.ModelID)
		}
	}

	if got[0].ModelID != "cline-free/muse-spark-1.3-contributor" {
		t.Errorf("unexpected first model %q", got[0].ModelID)
	}
	if got[1].ModelID != "deepseek/deepseek-v4-flash" {
		t.Errorf("unexpected second model %q", got[1].ModelID)
	}
}

func TestClineFreeAdapterRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTooManyRequests)
	}))
	defer server.Close()

	oldURL := ClineRecommendedModelsURL
	ClineRecommendedModelsURL = server.URL
	defer func() { ClineRecommendedModelsURL = oldURL }()

	_, err := NewClineFreeAdapter(server.Client()).Discover(context.Background())
	if err == nil {
		t.Fatal("expected non-200 error")
	}
}
