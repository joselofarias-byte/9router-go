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

// TestTrustManager_ModelNotFoundImmediateQuarantine verifies a model-not-found
// failure quarantines a node immediately like auth/permanent failures do,
// rather than waiting for the consecutive-failure threshold — the exact
// provider/model/account combination will not start working on retry.
func TestTrustManager_ModelNotFoundImmediateQuarantine(t *testing.T) {
	tm := NewManager()

	tm.RecordObservation("prov", "bad-model", "acc", true, "")
	if lvl := tm.GetTrustLevel("prov", "bad-model", "acc"); lvl != TrustVerified {
		t.Fatalf("expected Verified before failure, got %s", lvl)
	}

	tm.RecordObservation("prov", "bad-model", "acc", false, providers.ErrModelNotFound)
	if lvl := tm.GetTrustLevel("prov", "bad-model", "acc"); lvl != TrustQuarantined {
		t.Errorf("expected immediate Quarantined on model_not_found, got %s", lvl)
	}
}

func TestTrustManager_LatencyStats(t *testing.T) {
	tm := NewManager()

	if _, hasData := tm.LatencyStats("prov", "mod", "acc"); hasData {
		t.Fatal("expected no latency data before any probe")
	}

	tm.RecordLatency("prov", "mod", "acc", 100)
	tm.RecordLatency("prov", "mod", "acc", 300)

	avg, hasData := tm.LatencyStats("prov", "mod", "acc")
	if !hasData {
		t.Fatal("expected latency data after recording samples")
	}
	if avg != 200 {
		t.Errorf("expected average 200ms, got %d", avg)
	}

	// A different node's latency must not be affected.
	if _, hasData := tm.LatencyStats("prov", "mod", "other-acc"); hasData {
		t.Error("expected no latency data for an unrelated node")
	}

	// Negative latency (invalid sample) must be ignored.
	tm.RecordLatency("prov", "mod", "acc", -5)
	if avg, _ := tm.LatencyStats("prov", "mod", "acc"); avg != 200 {
		t.Errorf("expected average to remain 200ms after ignoring a negative sample, got %d", avg)
	}
}
