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
