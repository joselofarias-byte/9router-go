package edition

import "testing"

func TestCommunityProviderFailsClosed(t *testing.T) {
	var p Provider = CommunityProvider{}
	for _, capability := range []Capability{
		CapabilityAdvancedRouting,
		CapabilityMultiAccountPolicy,
		CapabilityIntentProfiles,
		CapabilityAdvancedTelemetry,
		CapabilityBudgetPolicy,
		CapabilityQuotaExpiry,
	} {
		if p.Enabled(capability) {
			t.Fatalf("community unexpectedly enabled %q", capability)
		}
	}
	status := p.Status()
	if status.Mode != "community" {
		t.Fatalf("mode = %q, want community", status.Mode)
	}
	if len(status.Features) != 0 {
		t.Fatalf("community exposed commercial features: %v", status.Features)
	}
}

func TestPublicPlanNamesStayStable(t *testing.T) {
	got := []Plan{PlanFree, PlanBetaPro, PlanSupporter, PlanPro, PlanBusiness}
	want := []Plan{"free", "beta_pro", "supporter", "pro", "business"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plan[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
