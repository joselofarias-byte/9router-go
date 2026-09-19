package pools

import (
	"testing"

	"9router/proxy/internal/controlplane/registry"
)

func TestIsPoolAliases(t *testing.T) {
	for _, name := range []string{"fabric-free", "free-best", "free", "FREE-BEST", " Free "} {
		if !IsPool(name) {
			t.Fatalf("expected %q to be a pool", name)
		}
		if CanonicalName(name) != FabricFree {
			t.Fatalf("canonical(%q)=%q, want %s", name, CanonicalName(name), FabricFree)
		}
	}
	if IsPool("gpt-4") || IsPool("") {
		t.Fatal("exact models must not be treated as pools")
	}
}

func TestIsFreePricing(t *testing.T) {
	if IsFreePricing(nil) {
		t.Fatal("nil must not be free")
	}
	cases := []struct {
		mode string
		want bool
	}{
		{"free", true},
		{"free_tier", true},
		{"paid", false},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range cases {
		got := IsFreePricing(&registry.ProviderModel{PricingMode: tc.mode})
		if got != tc.want {
			t.Fatalf("mode %q: got %v want %v", tc.mode, got, tc.want)
		}
	}
}

func TestGetMember(t *testing.T) {
	p, ok := Get("free-best")
	if !ok || p.Name != FabricFree {
		t.Fatalf("Get(free-best)=%v ok=%v", p, ok)
	}
	if !p.Member(&registry.ProviderModel{PricingMode: "free"}) {
		t.Fatal("free member rejected")
	}
	if p.Member(&registry.ProviderModel{PricingMode: "paid"}) {
		t.Fatal("paid member accepted")
	}
}
