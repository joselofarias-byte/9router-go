package freecoding

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouterCatalogOmitsAuthorization(t *testing.T) {
	var authorization, apiKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		apiKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	got, err := discoverOpenRouterCatalog(t.Context(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("candidates %d", len(got))
	}
	if authorization != "" || apiKey != "" {
		t.Fatalf("public catalog sent credentials authorization=%q api_key=%q", authorization, apiKey)
	}
}
