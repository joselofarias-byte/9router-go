package scoring

import (
	"testing"

	"9router/proxy/internal/controlplane/trust"
)

func TestCalculateScoring(t *testing.T) {
	f := Factors{
		TrustLevel:   trust.TrustTrusted,
		LatencyMs:    1000,
		TTFTMs:       300,
		SuccessRate:  0.95,
		IsFreeTier:   true,
		BenchmarkAvg: 0.8,
	}

	s := Calculate(f)

	if !s.IsRoutable {
		t.Error("Expected to be routable")
	}

	if s.Total < 80 {
		t.Errorf("Expected high score, got %f", s.Total)
	}

	// Test Quarantined
	f.TrustLevel = trust.TrustQuarantined
	sq := Calculate(f)
	if sq.IsRoutable {
		t.Error("Quarantined node should not be routable")
	}
}
