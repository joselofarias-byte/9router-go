package runtime

import (
	"testing"

	"go.uber.org/fx"

	"9router/proxy/pkg/edition"
)

type testProvider struct{}

func (testProvider) Enabled(capability edition.Capability) bool {
	return capability == edition.CapabilityAdvancedRouting
}

func (testProvider) Status() edition.Status {
	return edition.Status{
		Mode: "licensed",
		Plan: edition.PlanPro,
		Features: []edition.Capability{
			edition.CapabilityAdvancedRouting,
		},
	}
}

func TestWithEditionProviderOverridesCommunity(t *testing.T) {
	var got edition.Provider
	app := fx.New(
		fx.Provide(func() edition.Provider { return edition.CommunityProvider{} }),
		WithEditionProvider(testProvider{}),
		fx.Invoke(func(provider edition.Provider) {
			got = provider
		}),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Enabled(edition.CapabilityAdvancedRouting) {
		t.Fatal("commercial provider did not replace Community")
	}
	if got.Status().Plan != edition.PlanPro {
		t.Fatalf("plan = %q, want %q", got.Status().Plan, edition.PlanPro)
	}
}

func TestWithNilEditionProviderStaysCommunity(t *testing.T) {
	var got edition.Provider
	app := fx.New(
		fx.Provide(func() edition.Provider { return edition.CommunityProvider{} }),
		WithEditionProvider(nil),
		fx.Invoke(func(provider edition.Provider) {
			got = provider
		}),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status().Mode != "community" {
		t.Fatalf("unexpected provider: %#v", got)
	}
}
