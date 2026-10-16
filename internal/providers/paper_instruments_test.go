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
