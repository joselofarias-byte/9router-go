package app

import (
	"go.uber.org/fx"

	"9router/proxy/pkg/edition"
)

// EditionModule installs Community as the fail-safe edition provider.
// Private product assembly can decorate this public interface without importing
// any 9router internal package.
var EditionModule = fx.Provide(func() edition.Provider {
	return edition.CommunityProvider{}
})
