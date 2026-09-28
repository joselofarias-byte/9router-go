package providers

import "testing"

func TestAPInexProviderRegistration(t *testing.T) {
	cfg, ok := KnownProviders["apinex"]
	if !ok {
		t.Fatal("KnownProviders has no apinex entry")
	}
	if cfg.BaseURL != "https://api.apinex.bond/v1/chat/completions" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.AuthHeader != "Authorization" || cfg.AuthScheme != "bearer" {
		t.Errorf("auth = %q/%q, want Authorization/bearer", cfg.AuthHeader, cfg.AuthScheme)
	}
	if cfg.NoAuth {
		t.Error("APInex requires an API key")
	}
	if got := ResolveAlias("apx"); got != "apinex" {
		t.Errorf("ResolveAlias(apx) = %q, want apinex", got)
	}
	if got := ModelsListURL("apinex"); got != "https://api.apinex.bond/v1/models" {
		t.Errorf("ModelsListURL(apinex) = %q", got)
	}

	risk := GetProviderRiskProfile("apinex")
	if risk.AccessMode != AccessAPIKey || risk.AccountRisk != AccountRiskLow {
		t.Errorf("unexpected risk profile: %+v", risk)
	}
}
