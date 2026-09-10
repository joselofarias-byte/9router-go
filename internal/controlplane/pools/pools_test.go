package pools

import (
	"testing"

	"9router/proxy/internal/controlplane/registry"
)

func TestIsPool(t *testing.T) {
	if !IsPool(FabricFree) {
		t.Errorf("expected %q to be a registered pool", FabricFree)
	}
	if IsPool("openai/gpt-4o") {
		t.Errorf("expected a concrete provider/model string not to be a pool")
	}
	if IsPool("fabric-best") {
		t.Errorf("fabric-best is intentionally unregistered this sprint")
	}
}

func TestFabricFreeMembership(t *testing.T) {
	pool, ok := Get(FabricFree)
	if !ok {
		t.Fatalf("expected fabric-free to be registered")
	}

	cases := []struct {
		name string
		pm   *registry.ProviderModel
		want bool
	}{
		{"free", &registry.ProviderModel{PricingMode: "free"}, true},
		{"free_tier", &registry.ProviderModel{PricingMode: "free_tier"}, true},
		{"paid", &registry.ProviderModel{PricingMode: "paid"}, false},
		{"unknown", &registry.ProviderModel{PricingMode: "unknown"}, false},
		{"empty", &registry.ProviderModel{PricingMode: ""}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pool.Member(c.pm); got != c.want {
				t.Errorf("Member(%v) = %v, want %v", c.pm, got, c.want)
			}
		})
	}
}
