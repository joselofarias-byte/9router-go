package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKiraAdapter_FreeChatOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"kira-free","object":"model","type":"chat","status":"active","is_free":true},
			{"id":"kira-paid","object":"model","type":"chat","status":"active","is_free":false},
			{"id":"kira-image","object":"model","type":"image","status":"active","is_free":true},
			{"id":"kira-down","object":"model","type":"chat","status":"disabled","is_free":true},
			{"id":"","object":"model","type":"chat","status":"active","is_free":true}
		]}`))
	}))
	defer srv.Close()

	got, err := NewKiraAdapter(srv.Client(), srv.URL).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ModelID != "kira-free" || got[0].PricingMode != "free_tier" {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}

func TestKiraAdapter_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[`))
	}))
	defer srv.Close()
	if _, err := NewKiraAdapter(srv.Client(), srv.URL).Discover(context.Background()); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestKiraAdapter_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := NewKiraAdapter(srv.Client(), srv.URL).Discover(context.Background()); err == nil {
		t.Fatal("expected status error")
	}
}
