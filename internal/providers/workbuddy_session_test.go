package providers

import "testing"

func TestWorkBuddySessionProviderRegistration(t *testing.T) {
	cfg, ok := KnownProviders[WorkBuddySessionProvider]
	if !ok {
		t.Fatal("workbuddy-session provider not registered")
	}
	if cfg.BaseURL != "local://codebuddy-session" || cfg.DefaultAPIKey != "session" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	for _, alias := range []string{"wbs", "wbf", "workbuddy-free", "workbuddy-session"} {
		if got := ResolveAlias(alias); got != WorkBuddySessionProvider {
			t.Fatalf("alias %q resolved to %q", alias, got)
		}
	}

	risk := GetProviderRiskProfile(WorkBuddySessionProvider)
	if risk.AccessMode != AccessProductSurface || risk.AccountRisk != AccountRiskMedium || risk.ProbePolicy != ProbeConservative || risk.ScorePenalty != 7 {
		t.Fatalf("unexpected risk profile: %+v", risk)
	}
}
