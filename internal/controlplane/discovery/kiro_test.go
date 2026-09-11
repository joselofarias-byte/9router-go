package discovery

import (
	"context"
	"testing"

	"9router/proxy/internal/providers"
)

func TestKiroStaticAdapter_DeclaresKnownFreeModels(t *testing.T) {
	adapter := NewKiroStaticAdapter()
	if adapter.SourceID() != "kiro-static" {
		t.Fatalf("unexpected SourceID: %s", adapter.SourceID())
	}

	got, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	want := providers.ModelsForProvider("kiro")
	if len(want) == 0 {
		t.Fatal("test setup: expected providers.ModelsForProvider(\"kiro\") to return known models")
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d candidates, got %d: %+v", len(want), len(got), got)
	}

	for _, c := range got {
		if c.ProviderID != "kiro" {
			t.Errorf("expected ProviderID kiro, got %s", c.ProviderID)
		}
		if c.PricingMode != "free" {
			t.Errorf("expected PricingMode free, got %s", c.PricingMode)
		}
		if c.ModelID == "" {
			t.Error("expected non-empty ModelID")
		}
	}
}
