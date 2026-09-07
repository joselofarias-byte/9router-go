package chat

import (
	"context"
	"database/sql"

	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/log"
)

// Legacy compatibility for Data Plane -> Control Plane transition
// In production, TrustManager should be global and stateful.
var globalTrustManager = trust.NewManager()
var globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager}

// getActiveCandidates resolves the request routing pool.
func getActiveCandidates(ctx context.Context, db *sql.DB, model string) []routing.RouteNode {
	if db != nil {
		// Sync is safely called on every request because it internally
		// uses a cheap generation-based check preventing full queries/locks
		// when no account mutations have occurred.
		err := cpsync.SyncAccountsFromDB(db)
		if err != nil {
			// If sync fails, do not route using potentially stale Control Plane state.
			// Return nil to force fallback to the legacy Data Plane DB lookup path.
			log.Error("routing", "control plane sync failed, aborting cp candidates", "error", err)
			return nil
		}
	}

	// For now, always use Balanced policy logic by default
	return globalRoutingEngine.SelectCandidates(model, routing.PolicyBalanced)
}
