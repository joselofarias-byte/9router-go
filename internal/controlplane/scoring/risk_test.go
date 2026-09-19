package scoring

import (
	"testing"

	"9router/proxy/internal/controlplane/trust"
)

func TestAccountRiskPenaltyLowersOtherwiseEqualScore(t *testing.T) {
	base := Factors{
		TrustLevel:   trust.TrustVerified,
		TTFTMs:       400,
		SuccessRate:  0.95,
		IsFreeTier:   true,
		BenchmarkAvg: 0.8,
	}

	lowRisk := Calculate(base)
	base.AccountRiskPenalty = 15
	highRisk := Calculate(base)

	if !lowRisk.IsRoutable || !highRisk.IsRoutable {
		t.Fatal("risk penalty should not make a healthy node unroutable")
	}
	if lowRisk.Total-highRisk.Total != 15 {
		t.Fatalf("expected 15-point deduction, got low=%v high=%v", lowRisk.Total, highRisk.Total)
	}
	if highRisk.Dimensions["account_risk_penalty"] != -15 {
		t.Fatalf("expected account_risk_penalty dimension -15, got %v", highRisk.Dimensions["account_risk_penalty"])
	}
}
