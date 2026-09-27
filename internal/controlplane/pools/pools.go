// Package pools defines Fabric logical pools: virtual model names that
// expand to heterogeneous, dynamically-scored provider/model candidates
// instead of a single fixed upstream model.
//
// A pool is not a DB combo and not a static alias. Membership is evaluated
// live against the in-memory registry snapshot, so health, quarantine,
// pricing, and account state changes take effect on the next request.
package pools

import (
	"strings"

	"9router/proxy/internal/controlplane/registry"
)

// FabricFree is the canonical logical pool that expands to every currently
// eligible free/free-tier route across discovered providers.
const FabricFree = "fabric-free"

// FreeBest and Free are public virtual-model aliases for FabricFree.
// They coexist as names and resolve through the same pool implementation so
// OpenCode and other clients can request either identifier.
const (
	FreeBest = "free-best"
	Free     = "free"
)

// Pool describes a logical pool: a name and the membership predicate that
// decides whether a given ProviderModel belongs to it.
type Pool struct {
	Name string
	// Member reports whether pm is eligible for this pool. It must not
	// perform I/O — pool membership is evaluated against the in-memory
	// registry snapshot only.
	Member func(pm *registry.ProviderModel) bool
}

// IsFreePricing reports whether a ProviderModel's PricingMode is one of the
// two free classifications produced by discovery adapters. Anything else
// ("paid", "unknown", or empty) is excluded — fabric-free never silently
// routes to a paid or unclassified route.
func IsFreePricing(pm *registry.ProviderModel) bool {
	return pm != nil && (pm.PricingMode == "free" || pm.PricingMode == "free_tier")
}

var registered = map[string]Pool{
	FabricFree: {Name: FabricFree, Member: IsFreePricing},
	FreeBest:   {Name: FreeBest, Member: IsFreePricing},
	Free:       {Name: Free, Member: IsFreePricing},
}

// CanonicalName maps any registered pool alias to FabricFree.
// Unknown names return empty string.
func CanonicalName(name string) string {
	if name == "" {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if _, ok := registered[key]; ok {
		return FabricFree
	}
	return ""
}

// IsPool reports whether name refers to a registered Fabric logical pool
// (including free-best / free aliases).
func IsPool(name string) bool {
	return CanonicalName(name) != ""
}

// Get returns the pool definition for name (alias-aware), or false if name
// is not a registered pool.
func Get(name string) (Pool, bool) {
	canon := CanonicalName(name)
	if canon == "" {
		return Pool{}, false
	}
	p, ok := registered[canon]
	return p, ok
}

// Names returns the public identifiers that resolve to the free pool.
func Names() []string {
	return []string{FabricFree, FreeBest, Free}
}
