package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLlamaCppAdapterDiscoversModelsAsLocalFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen-local"},{"id":"qwen-local"},{"id":"coder-local"}]}`))
	}))
	defer srv.Close()

	got, err := NewLlamaCppAdapter(srv.Client(), srv.URL+"/v1").Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates: %+v", len(got), got)
	}
	for _, c := range got {
		if c.ProviderID != "llamacpp" || c.PricingMode != "free" || c.UpstreamModel != c.ModelID {
			t.Fatalf("unexpected candidate: %+v", c)
		}
	}
}

func TestLlamaCppAdapterRejectsMalformedCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":"not-a-list"}`))
	}))
	defer srv.Close()

	if _, err := NewLlamaCppAdapter(srv.Client(), srv.URL).Discover(context.Background()); err == nil {
		t.Fatal("expected malformed llama.cpp catalog to fail")
	}
}

func TestNormalizeLlamaCppRootURL(t *testing.T) {
	for input, want := range map[string]string{
		"": "http://127.0.0.1:8080",
		"http://127.0.0.1:9090/": "http://127.0.0.1:9090",
		"http://127.0.0.1:9090/v1": "http://127.0.0.1:9090",
		"http://127.0.0.1:9090/v1/chat/completions": "http://127.0.0.1:9090",
	} {
		if got := normalizeLlamaCppRootURL(input); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}
