package discovery

import "testing"

func TestExplicitFreeModelID(t *testing.T) {
	free := []string{
		"deepseek-v4.1-flash:free",
		"kilo-auto/free",
		"jev-1.13-free",
		"goldeneye-free-auto",
	}
	for _, id := range free {
		if !explicitFreeModelID(id) {
			t.Errorf("%q should be explicitly free", id)
		}
	}
	for _, id := range []string{"freeform-1", "claude-sonnet-5", "mimo-v2.5-pro"} {
		if explicitFreeModelID(id) {
			t.Errorf("%q must not be inferred free", id)
		}
	}
}

func TestBuiltinFreeAdapterIncludesZeroCredentialRoutes(t *testing.T) {
	got, err := NewBuiltinFreeAdapter().Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, c := range got {
		if c.PricingMode != "free" {
			t.Fatalf("%s/%s pricing=%q", c.ProviderID, c.UpstreamModel, c.PricingMode)
		}
		found[c.ProviderID+"/"+c.UpstreamModel] = true
	}

	for _, want := range []string{
		"mimo-free/mimo-auto",
		"opencode/jev-1.13-free",
		"opencode/muse-spark-1.3-contributor-free",
	} {
		if !found[want] {
			t.Errorf("missing built-in free route %s", want)
		}
	}
}
