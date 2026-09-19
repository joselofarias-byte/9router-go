package providers

import "testing"

func TestProviderRiskProfiles(t *testing.T) {
	tests := []struct {
		provider string
		mode     AccessMode
		risk     AccountRisk
		penalty  float64
		probe    ProbePolicy
	}{
		{"cline", AccessAPIKey, AccountRiskLow, 0, ProbeStandard},
		{"codebuddy-intl", AccessAPIKey, AccountRiskLow, 0, ProbeStandard},
		{"antigravity", AccessOAuthAccount, AccountRiskHigh, 15, ProbeConservative},
		{"qoder", AccessProductSurface, AccountRiskMedium, 7, ProbeConservative},
		{"codebuddy-cn", AccessProductSurface, AccountRiskMedium, 7, ProbeConservative},
		{"mimo-free", AccessNoAuth, AccountRiskLow, 0, ProbeStandard},
		{"provider-that-does-not-exist", AccessUnknown, AccountRiskMedium, 0, ProbeConservative},
	}

	for _, tc := range tests {
		got := GetProviderRiskProfile(tc.provider)
		if got.AccessMode != tc.mode || got.AccountRisk != tc.risk || got.ScorePenalty != tc.penalty || got.ProbePolicy != tc.probe {
			t.Errorf("%s: expected mode=%s risk=%s penalty=%v probe=%s, got %+v", tc.provider, tc.mode, tc.risk, tc.penalty, tc.probe, got)
		}
	}
}
