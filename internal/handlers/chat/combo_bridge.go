package chat

import (
	"context"
	"database/sql"

	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/log"
)

// Legacy compatibility for Data Plane -> Control Plane transition.
// In production, TrustManager should be global and stateful.
var globalTrustManager = trust.NewManager()
var globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager}

// getActiveCandidates resolves exact-model candidates using the default balanced policy.
func getActiveCandidates(ctx context.Context, db *sql.DB, model string) []routing.RouteNode {
	return getPolicyCandidates(ctx, db, model, routing.PolicyBalanced)
}

// getPolicyCandidates resolves either an exact model or a dynamic policy pool.
// Passing model="" lets the policy select across all discovered models.
func getPolicyCandidates(ctx context.Context, db *sql.DB, model string, policy routing.Policy) []routing.RouteNode {
	if db != nil {
		// Sync is safely called on every request because it internally uses a cheap
		// generation-based check preventing full queries/locks when accounts did not change.
		err := cpsync.SyncAccountsFromDB(db)
		if err != nil {
			log.Error("routing", "control plane sync failed, aborting cp candidates", "error", err)
			return nil
		}
	}

	return globalRoutingEngine.SelectCandidates(model, policy)
}
