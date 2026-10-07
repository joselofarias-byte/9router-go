package trust

import (
	"testing"
	"time"

	"9router/proxy/internal/providers"
)

func TestTrustManager(t *testing.T) {
	tm := NewManager()

	// Initial unknown state
	if lvl := tm.GetTrustLevel("prov", "mod", "acc"); lvl != TrustUnknown {
		t.Errorf("Expected Unknown, got %s", lvl)
	}

	// Success -> Verified
	tm.RecordObservation("prov", "mod", "acc", true, "")
	if lvl := tm.GetTrustLevel("prov", "mod", "acc"); lvl != TrustVerified {
		t.Errorf("Expected Verified, got %s", lvl)
	}

	// Auth failure -> Immediate Quarantine
	tm.RecordObservation("prov", "mod", "acc", false, providers.ErrAuth)
	if lvl := tm.GetTrustLevel("prov", "mod", "acc"); lvl != TrustQuarantined {
		t.Errorf("Expected Quarantined, got %s", lvl)
	}

	// Simulate time passing (fast forward)
	tm.mu.Lock()
	tm.records["prov|mod|acc"].QuarantineUntil = time.Now().Add(-1 * time.Second)
	tm.mu.Unlock()

	// Auto recover to degraded
	if lvl := tm.GetTrustLevel("prov", "mod", "acc"); lvl != TrustDegraded {
		t.Errorf("Expected Degraded, got %s", lvl)
	}
}


func TestRequestOutcomeStats(t *testing.T) {
	tm := NewManager()

	tm.RecordRequestOutcome("prov", "mod", "acc", true, "", 120, 40)
	tm.RecordRequestOutcome("prov", "mod", "acc", false, providers.ErrRateLimit, 280, 80)

	stats := tm.GetRequestStats("prov", "mod", "acc")
	if stats.Samples != 2 {
		t.Fatalf("expected 2 samples, got %d", stats.Samples)
	}
	if stats.SuccessRate != 0.5 {
		t.Fatalf("expected success rate 0.5, got %f", stats.SuccessRate)
	}
	if stats.AvgLatencyMs != 200 {
		t.Fatalf("expected avg latency 200ms, got %d", stats.AvgLatencyMs)
	}
	if stats.AvgTTFTMs != 60 {
		t.Fatalf("expected avg TTFT 60ms, got %d", stats.AvgTTFTMs)
	}
}

func TestRequestOutcomeStatsIgnoresMissingTTFT(t *testing.T) {
	tm := NewManager()

	tm.RecordRequestOutcome("prov", "mod", "acc", true, "", 100, 0)
	tm.RecordRequestOutcome("prov", "mod", "acc", true, "", 200, 50)

	stats := tm.GetRequestStats("prov", "mod", "acc")
	if stats.AvgLatencyMs != 150 {
		t.Fatalf("expected avg latency 150ms, got %d", stats.AvgLatencyMs)
	}
	if stats.AvgTTFTMs != 50 {
		t.Fatalf("expected TTFT average to use observed samples only, got %d", stats.AvgTTFTMs)
	}
}
