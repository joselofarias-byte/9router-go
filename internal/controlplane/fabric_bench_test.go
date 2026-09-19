package controlplane_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"

	"9router/proxy/internal/controlplane/discovery"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/scoring"
	"9router/proxy/internal/controlplane/trust"
)

func seedBench(b *testing.B, n int) *routing.Engine {
	b.Helper()
	registry.InitRegistry(nil)
	state := registry.GetActiveState()
	for i := 0; i < n; i++ {
		id := "p" + strconv.Itoa(i)
		state.Providers[id] = &registry.Provider{ID: id, IsActive: true}
		state.ProviderModels[id] = map[string]*registry.ProviderModel{
			"m": {ProviderID: id, ModelID: "m", PricingMode: "free", IsActive: true},
		}
		state.Accounts["a"+id] = &registry.Account{ID: "a" + id, ProviderID: id, IsActive: true}
	}
	return &routing.Engine{TrustManager: trust.NewManager()}
}

func BenchmarkScoringCalculate(b *testing.B) {
	f := scoring.Factors{
		TrustLevel:         trust.TrustVerified,
		SuccessRate:        0.9,
		TTFTMs:             400,
		IsFreeTier:         true,
		AccountRiskPenalty: 7,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = scoring.Calculate(f)
	}
}

func BenchmarkSelectCandidatesFreeBest(b *testing.B) {
	engine := seedBench(b, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.SelectCandidates("free-best", routing.PolicyFreeOnly)
	}
}

func BenchmarkSnapshotRead(b *testing.B) {
	seedBench(b, 16)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if registry.GetActiveState() == nil {
			b.Fatal("nil state")
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func BenchmarkDiscoveryKiraParse(b *testing.B) {
	payload := []byte(`{"data":[{"id":"a","object":"model","type":"chat","status":"active","is_free":true}]}`)
	adapter := discovery.NewKiraAdapter(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
		}, nil
	})}, "http://bench")
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := adapter.Discover(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
