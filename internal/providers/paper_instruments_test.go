package providers

import "testing"

func TestPaperInstrumentsRequiresExplicitCredentials(t *testing.T) {
	cfg, ok := KnownProviders["paper-instruments"]
	if !ok {
		t.Fatal("paper-instruments provider is not registered")
	}
	if cfg.BaseURL != "https://api.paperinstruments.com/v1/chat/completions" {
		t.Fatalf("unexpected base URL: %q", cfg.BaseURL)
	}
	if cfg.AuthHeader != "Authorization" || cfg.AuthScheme != "bearer" {
		t.Fatalf("unexpected auth configuration: %q / %q", cfg.AuthHeader, cfg.AuthScheme)
	}
	if cfg.NoAuth || cfg.DefaultAPIKey != "" {
		t.Fatal("paid provider must not be usable without configured credentials")
	}
}

func TestPaperInstrumentsModelAlias(t *testing.T) {
	if got := ResolveAlias("paper"); got != "paper-instruments" {
		t.Fatalf("paper alias resolves to %q, want paper-instruments", got)
	}
	if got := GetProviderAlias("paper-instruments"); got != "paper" {
		t.Fatalf("published alias = %q, want paper", got)
	}
	models := GetProviderModels("paper")
	if len(models) != 1 || models[0] != "ultramarine-1-flash" {
		t.Fatalf("unexpected Paper Instruments models: %v", models)
	}
	if IsNoAuthProvider("paper") {
		t.Fatal("paper alias must not allow anonymous access")
	}
	if IsFreeTierModel("ultramarine-1-flash") {
		t.Fatal("paid Ultramarine model must not be classified as free tier")
	}
}
