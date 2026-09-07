package chat

import (
	"context"

	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/controlplane/trust"
)

// Legacy compatibility for Data Plane -> Control Plane transition
// In production, TrustManager should be global and stateful.
var globalTrustManager = trust.NewManager()
var globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager}

// getActiveCandidates resolves the request routing pool.
func getActiveCandidates(ctx context.Context, model string) []routing.RouteNode {
	// For now, always use Balanced policy logic by default
	return globalRoutingEngine.SelectCandidates(model, routing.PolicyBalanced)
}
