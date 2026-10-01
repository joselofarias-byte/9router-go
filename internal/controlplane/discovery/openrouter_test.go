package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOpenRouterOfficialCatalogFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		wantErr          bool
	}{
		{"free", `{"data":[{"id":"qwen/coder:free","pricing":{"prompt":"0","completion":"0","request":"0"},"context_length":262144,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools","reasoning"]}]}`, "free_tier", false},
		{"suffix-with-price", `{"data":[{"id":"qwen/coder:free","pricing":{"prompt":"0.01","completion":"0"},"architecture":{"output_modalities":["text"]}}]}`, "paid", false},
		{"missing-price", `{"data":[{"id":"qwen/coder:free","pricing":{"prompt":"0"},"architecture":{"output_modalities":["text"]}}]}`, "paid", false},
		{"invalid-price", `{"data":[{"id":"qwen/coder:free","pricing":{"prompt":"NaN","completion":"0"},"architecture":{"output_modalities":["text"]}}]}`, "paid", false},
		{"additional-fee", `{"data":[{"id":"qwen/coder:free","pricing":{"prompt":"0","completion":"0","web_search":"0.01"},"architecture":{"output_modalities":["text"]}}]}`, "paid", false},
		{"tiered-pricing", `{"data":[{"id":"qwen/coder:free","pricing":{"prompt":"0","completion":"0","overrides":[{"prompt":"0.001"}]},"architecture":{"output_modalities":["text"]}}]}`, "paid", false},
		{"empty", `{"data":[]}`, "", false},
		{"no-data", `{"error":"bad"}`, "", true},
		{"null-data", `{"data":null}`, "", true},
		{"non-chat", `{"data":[{"id":"image:free","pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["image"]}}]}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("public catalog sent a credential")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			a := NewOpenRouterAdapter(srv.Client())
			a.url = srv.URL
			got, err := a.Discover(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected catalog: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].PricingMode != tc.want || got[0].UpstreamModel != "qwen/coder:free" {
				t.Fatalf("unexpected catalog: %+v", got)
			}
			if tc.name == "free" && (!strings.Contains(got[0].Capabilities, `"tools":true`) || !strings.Contains(got[0].Capabilities, `262144`)) {
				t.Fatalf("capabilities lost: %s", got[0].Capabilities)
			}
		})
	}
}

func TestOpenRouterOfficialCatalogLive(t *testing.T) {
	if os.Getenv("OPENROUTER_CATALOG_LIVE") != "1" {
		t.Skip("public catalog check is opt-in")
	}
	got, err := NewOpenRouterAdapter(nil).Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	free := 0
	for _, c := range got {
		if c.PricingMode == "free_tier" {
			free++
			t.Logf("free catalog %s capabilities=%s", c.UpstreamModel, c.Capabilities)
		}
	}
	if free == 0 {
		t.Fatal("official catalog has no confirmed free variants")
	}
	t.Logf("total=%d free=%d; catalog only, no inference", len(got), free)
}

func TestOpenRouterSetCatalogURL(t *testing.T) {
	a := NewOpenRouterAdapter(nil)
	if a.url != OpenRouterModelsURL {
		t.Fatalf("default url %s", a.url)
	}
	a.SetCatalogURL("http://fixture.example/models")
	if a.url != "http://fixture.example/models" {
		t.Fatalf("fixture url %s", a.url)
	}
	a.SetCatalogURL("  ")
	if a.url != OpenRouterModelsURL {
		t.Fatalf("restored url %s", a.url)
	}
}

func TestOpenRouterCatalogFailureDoesNotReplacePreviousClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	a := NewOpenRouterAdapter(srv.Client())
	a.url = srv.URL
	if _, err := a.Discover(context.Background()); err == nil {
		t.Fatal("503 accepted as empty catalog")
	}
}
