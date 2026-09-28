package discovery

import (
	"context"
	"testing"
)

func TestKiroStaticAdapter_UsesRegistry(t *testing.T) {
	got, err := NewKiroStaticAdapter().Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected kiro registry models")
	}
	for _, c := range got {
		if c.ProviderID != "kiro" || c.PricingMode != "free" || c.ModelID == "" {
			t.Fatalf("unexpected candidate %+v", c)
		}
	}
}
