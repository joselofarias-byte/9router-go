package chat

import (
	"context"
	"errors"
	"net/http"
	"time"

	"9router/proxy/internal/controlplane/availability"
	"9router/proxy/internal/controlplane/capacity"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/providers"
)

type freeProfileContextKey struct{}

func withFreeProfile(ctx context.Context, name string, enabled bool) context.Context {
	if !enabled {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !routing.IsFreeProfile(name) {
		name = "free-best"
	}
	return context.WithValue(ctx, freeProfileContextKey{}, canonicalVirtualName(name))
}

func (h *ChatHandler) allowFreeProfileHop(ctx context.Context, enabled bool, entry string) bool {
	if !enabled {
		return true
	}
	if !h.freeEntryStillEligible(entry) {
		return false
	}
	profile, _ := ctx.Value(freeProfileContextKey{}).(string)
	return h.declaredProfileStillAllows(profile, entry)
}

// recordVirtualFreeTraffic stores health for free catalog identities only.
// Bad requests, cancellations, and explicit user combos do not poison it.
// A 429 cooldown is recorded separately from any reported balance.
func recordVirtualFreeTraffic(ctx context.Context, provider, wireModel, connectionID string, err error, latencyMs, ttftMs int) {
	if ctx == nil {
		return
	}
	if _, ok := ctx.Value(freeProfileContextKey{}).(string); !ok || ctx.Err() != nil {
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	account := capacityAccountID(provider, connectionID)
	if account == "" {
		return
	}
	success, category, cooldown, ok := classifyFreeOutcome(err)
	if !ok {
		return
	}
	if !success && category == providers.ErrQuota {
		capacity.ObserveCooldown(provider, account, wireModel, cooldown)
	}
	observeFreeCatalog(provider, wireModel, account, success, category, cooldown, latencyMs, ttftMs)
}

func classifyFreeOutcome(err error) (success bool, category providers.ErrorCategory, cooldown time.Duration, ok bool) {
	if err == nil {
		return true, "", 0, true
	}
	var upstream *upstreamError
	if !errors.As(err, &upstream) {
		return false, providers.ErrTransient, 2 * time.Second, true
	}
	switch upstream.StatusCode {
	case http.StatusTooManyRequests, http.StatusPaymentRequired:
		cooldown = time.Minute
		if until, parseErr := time.Parse(time.RFC3339, extractRetryAfter(upstream.Body)); parseErr == nil {
			if wait := time.Until(until); wait > cooldown {
				cooldown = wait
			}
		}
		return false, providers.ErrQuota, cooldown, true
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, providers.ErrAuth, 5 * time.Minute, true
	case http.StatusNotFound:
		return false, providers.ErrPermanent, 5 * time.Minute, true
	default:
		if upstream.StatusCode < 500 {
			return false, "", 0, false
		}
		return false, providers.ErrTransient, 2 * time.Second, true
	}
}

func observeFreeCatalog(provider, wireModel, account string, success bool, category providers.ErrorCategory, cooldown time.Duration, latencyMs, ttftMs int) {
	state := registry.GetActiveState()
	if state == nil {
		return
	}
	for _, pm := range state.ProviderModels[provider] {
		if !freeCatalogMatch(pm, provider, wireModel) {
			continue
		}
		globalAvailability.Observe(availability.Key{Provider: provider, Model: pm.ModelID, Account: account}, success, string(category), cooldown, latencyMs, ttftMs)
		globalTrustManager.RecordObservation(provider, pm.ModelID, account, success, category)
	}
}

func freeCatalogMatch(pm *registry.ProviderModel, provider, wireModel string) bool {
	if pm == nil || !pm.IsActive || pm.ProviderID != provider || !routing.IsFreePricing(pm.PricingMode) {
		return false
	}
	wire := pm.UpstreamModel
	if wire == "" {
		wire = pm.ModelID
	}
	return wire == wireModel
}

// accountRouteEligible rechecks the exact account before forwarding.
// Model-level free entries dedupe accounts, so a sibling must not resurrect
// a blocked connection. Outside a free profile, only reported quota and
// observed cooldown apply.
func (h *ChatHandler) accountRouteEligible(ctx context.Context, provider, wire, account string) bool {
	if !capacity.Eligible(capacity.Inspect(provider, account, wire)) {
		return false
	}
	if ctx == nil {
		return true
	}
	if _, free := ctx.Value(freeProfileContextKey{}).(string); !free {
		return true
	}
	return freeAccountAvailable(provider, wire, account)
}

// accountCapacityAvailable is the connection-selector view of the same gate.
// A wire that is not a free catalog identity keeps its existing selection.
func accountCapacityAvailable(provider, wire, account string) bool {
	if !capacity.Eligible(capacity.Inspect(provider, account, wire)) {
		return false
	}
	state := registry.GetActiveState()
	if state == nil {
		return true
	}
	matched := false
	for _, pm := range state.ProviderModels[provider] {
		if !freeCatalogMatch(pm, provider, wire) {
			continue
		}
		matched = true
		if globalAvailability.Available(availability.Key{Provider: provider, Model: pm.ModelID, Account: account}) {
			return true
		}
	}
	return !matched
}

func freeAccountAvailable(provider, wire, account string) bool {
	state := registry.GetActiveState()
	if state == nil {
		return false
	}
	for _, pm := range state.ProviderModels[provider] {
		if !freeCatalogMatch(pm, provider, wire) {
			continue
		}
		if globalAvailability.Available(availability.Key{Provider: provider, Model: pm.ModelID, Account: account}) {
			return true
		}
	}
	return false
}

func capacityAccountID(provider, connectionID string) string {
	if cfg, ok := providers.KnownProviders[provider]; ok && cfg.NoAuth && (connectionID == "" || connectionID == "default" || connectionID == "noauth") {
		return routing.VirtualNoAuthAccountID(provider)
	}
	return connectionID
}
