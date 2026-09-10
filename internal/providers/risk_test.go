package providers

import "testing"

func TestProviderRiskProfiles(t *testing.T) {
	tests := []struct {
		provider string
		mode     AccessMode
		risk     AccountRisk
		penalty  float64
	}{
		{"cline", AccessAPIKey, AccountRiskLow, 0},
		{"antigravity", AccessOAuthAccount, AccountRiskHigh, 15},
		{"qoder", AccessProductSurface, AccountRiskMedium, 7},
		{"codebuddy-cn", AccessProductSurface, AccountRiskMedium, 7},
		{"mimo-free", AccessNoAuth, AccountRiskLow, 0},
		{"provider-that-does-not-exist", AccessUnknown, AccountRiskMedium, 0},
	}

	for _, tc := range tests {
		got := GetProviderRiskProfile(tc.provider)
		if got.AccessMode != tc.mode || got.AccountRisk != tc.risk || got.ScorePenalty != tc.penalty {
			t.Errorf("%s: expected mode=%s risk=%s penalty=%v, got %+v", tc.provider, tc.mode, tc.risk, tc.penalty, got)
		}
	}
}
