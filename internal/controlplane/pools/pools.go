// Package pools defines Fabric logical pools: virtual model names that
// expand to a set of heterogeneous, dynamically-scored provider/model
// candidates instead of a single fixed upstream model.
//
// A pool is NOT a model alias and NOT a combo stored in the DB — its
// membership is evaluated live against the Control Plane registry on every
// resolution, so health/quarantine/pricing changes are reflected immediately
// without any snapshot of a specific model list going stale.
package pools

import "9router/proxy/internal/controlplane/registry"

// FabricFree is the logical pool name that expands to every currently
// eligible free/free-tier route across all discovered providers.
const FabricFree = "fabric-free"

// Pool describes a logical pool: a name and the membership predicate that
// decides whether a given ProviderModel belongs to it.
type Pool struct {
	Name string
	// Member reports whether pm is eligible for this pool. It must not
	// perform I/O — pool membership is evaluated against the in-memory
	// registry snapshot only.
	Member func(pm *registry.ProviderModel) bool
}

// isFreePricing reports whether a ProviderModel's PricingMode is one of the
// two free classifications produced by discovery adapters. Anything else
// ("paid", "unknown", or empty) is excluded — fabric-free never silently
// routes to a paid or unclassified route.
func isFreePricing(pm *registry.ProviderModel) bool {
	return pm != nil && (pm.PricingMode == "free" || pm.PricingMode == "free_tier")
}

// registry of known logical pools. Only fabric-free is implemented for this
// sprint; the map shape is what makes adding fabric-best/fabric-coding later
// a one-line addition instead of a new code path.
var registered = map[string]Pool{
	FabricFree: {Name: FabricFree, Member: isFreePricing},
}

// IsPool reports whether name refers to a registered Fabric logical pool.
func IsPool(name string) bool {
	_, ok := registered[name]
	return ok
}

// Get returns the pool definition for name, or false if name is not a
// registered pool.
func Get(name string) (Pool, bool) {
	p, ok := registered[name]
	return p, ok
}
