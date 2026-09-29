package chat

import (
	"context"
	"errors"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/providers"
)

// recordVirtualFreeTraffic feeds actual free-pool traffic into the same trust
// state used by discovery and probes. The wire model may differ from the
// registry ID (for example a :free catalog alias), so update the matching
// registry node rather than inventing a second trust identity.
func recordVirtualFreeTraffic(ctx context.Context, provider, wireModel, connectionID string, err error, latencyMs int) {
	if !virtualFreeFromContext(ctx) || ctx.Err() != nil {
		return
	}
	accountID := connectionID
	if accountID == "" {
		if cfg, ok := providers.KnownProviders[provider]; ok && cfg.NoAuth {
			accountID = routing.VirtualNoAuthAccountID(provider)
		}
	}
	if accountID == "" {
		return
	}

	var status int
	var body []byte
	if err != nil {
		var upstream *upstreamError
		if !errors.As(err, &upstream) {
			return
		}
		status, body = upstream.StatusCode, upstream.Body
		cls := providers.ClassifyError(status, extractErrorText(body), 0)
		if !cls.ShouldFallback || status == StatusClientClosedRequest {
			return
		}
	}
	state := registry.GetActiveState()
	if state == nil {
		return
	}
	for _, model := range state.ProviderModels[provider] {
		if model == nil || !model.IsActive || model.ProviderID != provider || !pools.IsFreePricing(model) {
			continue
		}
		upstreamModel := model.UpstreamModel
		if upstreamModel == "" {
			upstreamModel = model.ModelID
		}
		if upstreamModel != wireModel {
			continue
		}
		recordRouteOutcome(provider, model.ModelID, accountID, err == nil, status, body, latencyMs)
	}
}
