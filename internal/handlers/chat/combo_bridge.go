package chat

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/controlplane/trust"
)

// Legacy compatibility for Data Plane -> Control Plane transition
// In production, TrustManager should be global and stateful.
var globalTrustManager = trust.NewManager()
var globalRoutingEngine = &routing.Engine{TrustManager: globalTrustManager}

var (
	lastSyncTime time.Time
	syncMu       sync.Mutex
)

// getActiveCandidates resolves the request routing pool.
func getActiveCandidates(ctx context.Context, db *sql.DB, model string) []routing.RouteNode {
	if db != nil {
		syncMu.Lock()
		// perform a cheap metadata sync if older than 5 seconds
		if time.Since(lastSyncTime) > 5*time.Second {
			cpsync.SyncAccountsFromDB(db)
			lastSyncTime = time.Now()
		}
		syncMu.Unlock()
	}

	// For now, always use Balanced policy logic by default
	return globalRoutingEngine.SelectCandidates(model, routing.PolicyBalanced)
}
