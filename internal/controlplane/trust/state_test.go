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

func TestTrustQuotaAndSession(t *testing.T) {
	tm := NewManager()
	tm.RecordQuota("p", "m", "a", time.Hour)
	if blocked, why := tm.IsUnavailable("p", "m", "a"); !blocked || why != "quota_exhausted" {
		t.Fatalf("quota: blocked=%v why=%s", blocked, why)
	}
	tm.RecordSessionExpired("p2", "m", "a")
	if blocked, why := tm.IsUnavailable("p2", "m", "a"); !blocked || why != "session_expired" {
		t.Fatalf("session: blocked=%v why=%s", blocked, why)
	}
}

func TestTrustStaleLatency(t *testing.T) {
	tm := NewManager()
	tm.RecordLatency("p", "m", "a", 120)
	if avg, ok := tm.LatencyStats("p", "m", "a"); !ok || avg != 120 {
		t.Fatalf("fresh latency avg=%d ok=%v", avg, ok)
	}
	tm.mu.Lock()
	tm.records["p|m|a"].LastLatencyAt = time.Now().Add(-2 * time.Hour)
	tm.mu.Unlock()
	if _, ok := tm.LatencyStats("p", "m", "a"); ok {
		t.Fatal("stale latency must be ignored")
	}
}
