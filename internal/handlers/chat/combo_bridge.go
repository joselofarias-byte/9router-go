package chat

import (
	"context"
	"database/sql"
	"os"
	"strings"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/log"
)

// Shared control-plane objects used by live routing, probes, and admin status.
var globalTrustManager = trust.NewManager()
var globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager}

func defaultRoutingPolicy() routing.Policy {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FABRIC_ROUTING_POLICY"))) {
	case string(routing.PolicyFreeOnly):
		return routing.PolicyFreeOnly
	case string(routing.PolicyFreeFirst):
		return routing.PolicyFreeFirst
	case string(routing.PolicyTrusted):
		return routing.PolicyTrusted
	default:
		return routing.PolicyBalanced
	}
}

// getActiveCandidates resolves request routing for an exact model or a Fabric pool.
func getActiveCandidates(ctx context.Context, db *sql.DB, model string) []routing.RouteNode {
	policy := defaultRoutingPolicy()
	if pools.IsPool(model) {
		policy = routing.PolicyFreeOnly
		model = pools.CanonicalName(model)
	}
	return getPolicyCandidates(ctx, db, model, policy)
}

// getPolicyCandidates resolves either an exact model, a pool name, or a
// policy-wide dynamic pool (model="").
func getPolicyCandidates(ctx context.Context, db *sql.DB, model string, policy routing.Policy) []routing.RouteNode {
	_ = ctx
	if db != nil {
		if err := cpsync.SyncAccountsFromDB(db); err != nil {
			log.Error("routing", "control plane sync failed, aborting cp candidates", "error", err)
			return nil
		}
	}
	return globalRoutingEngine.SelectCandidates(model, policy)
}
