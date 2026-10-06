package runtime

import (
	"go.uber.org/fx"

	internalapp "9router/proxy/internal/app"
	"9router/proxy/pkg/edition"
)

// NewApp assembles the public 9router gateway runtime using environment-backed
// defaults. Additional Fx options are the supported assembly point for a
// separate product/release module.
//
// External product code must depend only on public packages such as pkg/edition;
// it must not import 9router internal packages.
func NewApp(options ...fx.Option) *fx.App {
	return internalapp.NewApp(internalapp.DefaultCLIParams(), options...)
}

// Run starts the assembled gateway and blocks until normal shutdown.
// Product-specific CLI packaging can wrap this function without copying the
// public gateway internals.
func Run(options ...fx.Option) error {
	return internalapp.Run(NewApp(options...))
}

// WithEditionProvider replaces the fail-safe Community provider with another
// implementation of the public edition contract. Passing nil keeps Community.
func WithEditionProvider(provider edition.Provider) fx.Option {
	if provider == nil {
		provider = edition.CommunityProvider{}
	}
	return fx.Decorate(func(edition.Provider) edition.Provider {
		return provider
	})
}
