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
		"opencode/muse-spark-1.3-contributor-free",
	} {
		if !found[want] {
			t.Errorf("missing built-in free route %s", want)
		}
	}
	if found["opencode/jev-1.13-free"] {
		t.Error("systemone model jev-1.13-free leaked into the chat free pool")
	}
	if found["opencode-zen/mimo-v2.5-free"] {
		t.Error("opencode-zen leaked into free-best before its multi-transport executor is ready")
	}
}

func TestIsChatProviderModelRejectsSystemOne(t *testing.T) {
	if isChatProviderModel("opencode", "jev-1.13-free") {
		t.Fatal("jev-1.13-free is systemone, not chat")
	}
	if !isChatProviderModel("opencode", "muse-spark-1.3-contributor-free") {
		t.Fatal("muse-spark contributor free should remain chat-capable")
	}
}


func TestNoAuthFreeProviderReady(t *testing.T) {
	if !noAuthFreeProviderReady("opencode") {
		t.Fatal("known ready free chat provider was rejected")
	}
	if noAuthFreeProviderReady("mimo-free") {
		t.Fatal("retired mimo-auto free lane must not be seeded")
	}
	if noAuthFreeProviderReady("opencode-zen") {
		t.Fatal("opencode-zen must stay fail-closed until its executor is reconciled")
	}
}


func TestBuiltinFreeAdapterScopesRetiredMimoForCleanup(t *testing.T) {
	found := false
	for _, providerID := range NewBuiltinFreeAdapter().ScopedProviderIDs() {
		if providerID == "mimo-free" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("mimo-free must remain scoped so stale builtin-free snapshots are deactivated")
	}
}
