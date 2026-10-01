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
	if !routing.IsFreeProfile(name) {
		name = "free-best"
	}
	return context.WithValue(ctx, freeProfileContextKey{}, canonicalVirtualName(name))
}

func (h *ChatHandler) profileEntryStillEligible(profile, entry string) bool {
	if profile == "" {
		return h.freeEntryStillEligible(entry)
	}
	candidates := getPolicyCandidates(nil, h.Repo.RawDB(), "", routing.PolicyFreeOnly)
	state := registry.GetActiveState()
	for _, current := range freeRouteEntries(state, routing.FilterProfile(state, candidates, profile)) {
		if current == entry {
			return true
		}
	}
	return false
}

func (h *ChatHandler) allowFreeProfileHop(ctx context.Context, enabled bool, entry string) bool {
	if !enabled {
		return true
	}
	profile, _ := ctx.Value(freeProfileContextKey{}).(string)
	return h.profileEntryStillEligible(profile, entry)
}

// Adapted from unmerged local work e6cd40c/f58724b (#23), against current
// main's APIs. Only free catalog identities receive observations. Never
// poison health for bad requests, cancellations or an explicit user combo.
func recordVirtualFreeTraffic(ctx context.Context, provider, wireModel, connectionID string, err error, latencyMs, ttftMs int) {
	if _, ok := ctx.Value(freeProfileContextKey{}).(string); !ok || ctx.Err() != nil {
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	account := connectionID
	if cfg, ok := providers.KnownProviders[provider]; ok && cfg.NoAuth && (account == "" || account == "default" || account == "noauth") {
		account = routing.VirtualNoAuthAccountID(provider)
	}
	if account == "" {
		return
	}
	success := err == nil
	var category providers.ErrorCategory
	var cooldown time.Duration
	if err != nil {
		var upstream *upstreamError
		if !errors.As(err, &upstream) {
			// Network errors before any response are a temporary failure.
			category, cooldown = providers.ErrTransient, 2*time.Second
		} else {
			switch upstream.StatusCode {
			case http.StatusTooManyRequests, http.StatusPaymentRequired:
				category, cooldown = providers.ErrQuota, time.Minute
				if until, parseErr := time.Parse(time.RFC3339, extractRetryAfter(upstream.Body)); parseErr == nil && time.Until(until) > cooldown {
					cooldown = time.Until(until)
				}
				capacity.ObserveCooldown(provider, account, wireModel, cooldown)
			case http.StatusUnauthorized, http.StatusForbidden:
				category, cooldown = providers.ErrAuth, 5*time.Minute
			case http.StatusNotFound:
				category, cooldown = providers.ErrPermanent, 5*time.Minute
			default:
				if upstream.StatusCode < 500 {
					return
				}
				category, cooldown = providers.ErrTransient, 2*time.Second
			}
		}
	}
	state := registry.GetActiveState()
	if state == nil {
		return
	}
	for _, pm := range state.ProviderModels[provider] {
		if pm == nil || !pm.IsActive || pm.ProviderID != provider || !routing.IsFreePricing(pm.PricingMode) {
			continue
		}
		wire := pm.UpstreamModel
		if wire == "" {
			wire = pm.ModelID
		}
		if wire != wireModel {
			continue
		}
		globalAvailability.Observe(availability.Key{Provider: provider, Model: pm.ModelID, Account: account}, success, string(category), cooldown, latencyMs, ttftMs)
		globalTrustManager.RecordObservation(provider, pm.ModelID, account, success, category)
	}
}

// Recheck the exact account before forwarding: model-level free entries dedupe
// accounts, so an eligible sibling must not resurrect a blocked connection.
func (h *ChatHandler) accountRouteEligible(ctx context.Context, provider, wire, account string) bool {
	if !capacity.Eligible(capacity.Inspect(provider, account, wire)) {
		return false
	}
	if _, free := ctx.Value(freeProfileContextKey{}).(string); !free {
		return true
	}
	state := registry.GetActiveState()
	if state == nil {
		return false
	}
	for _, pm := range state.ProviderModels[provider] {
		if pm == nil || !pm.IsActive || !routing.IsFreePricing(pm.PricingMode) {
			continue
		}
		upstream := pm.UpstreamModel
		if upstream == "" {
			upstream = pm.ModelID
		}
		if upstream == wire && globalAvailability.Available(availability.Key{Provider: provider, Model: pm.ModelID, Account: account}) {
			return true
		}
	}
	return false
}

// Legacy connection selection also honors health for the exact free catalog
// identities; unrelated explicit paid routes keep their existing behavior.
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
		if pm == nil || !pm.IsActive || !routing.IsFreePricing(pm.PricingMode) {
			continue
		}
		upstream := pm.UpstreamModel
		if upstream == "" {
			upstream = pm.ModelID
		}
		if upstream != wire {
			continue
		}
		matched = true
		if globalAvailability.Available(availability.Key{Provider: provider, Model: pm.ModelID, Account: account}) {
			return true
		}
	}
	return !matched
}
