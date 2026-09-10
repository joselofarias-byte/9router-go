package chat

import (
	"strings"

	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
)

// resolveFabricPool expands a Fabric logical pool (e.g. "fabric-free") into a
// ModelInfo whose ComboModels carry an ordered list of concrete "provider/model"
// entries produced by the Control Plane registry + scoring engine. It reuses
// the exact same combo-fallback execution path ordinary DB-defined combos use
// (handleComboFallback / handleMessagesComboFallback in combo.go), so a pool
// gets full account fallback, streaming-safe failover, cooldown/backoff
// locking and Retry-After handling for free — the Data Plane does not need a
// separate execution path for it.
//
// Returns nil if the pool has no eligible candidates right now (no free
// routes discovered, or all discovered free routes are quarantined/unhealthy/
// missing credentials); the caller should treat that as a resolution failure.
func (h *ChatHandler) resolveFabricPool(poolName string) *ModelInfo {
	if h.Repo != nil {
		if db := h.Repo.RawDB(); db != nil {
			if err := cpsync.SyncAccountsFromDB(db); err != nil {
				log.Error("fabric", "account sync failed, aborting pool resolution", "pool", poolName, "error", err)
				return nil
			}
		}
	}

	nodes := globalRoutingEngine.SelectCandidates(poolName, routing.PolicyFreeOnly)
	if len(nodes) == 0 {
		log.Warn("fabric", "no eligible candidates for pool", "pool", poolName)
		return nil
	}

	seen := make(map[string]bool, len(nodes))
	entries := make([]string, 0, len(nodes))
	for _, n := range nodes {
		key := n.ProviderID + "/" + n.ModelID
		if seen[key] {
			// Multiple accounts for the same provider/model: the highest-scored
			// one is already first since nodes are sorted descending, and
			// per-account fallback for this entry is handled inside
			// getBestConnection when the combo loop retries the same entry.
			continue
		}
		seen[key] = true
		entries = append(entries, key)
	}
	if len(entries) == 0 {
		return nil
	}

	first := strings.SplitN(entries[0], "/", 2)
	log.Info("fabric", "pool resolved", "pool", poolName, "candidates", len(entries), "top", entries[0])
	return &ModelInfo{
		Provider:    first[0],
		Model:       first[1],
		ComboModels: entries,
		Strategy:    "fallback",
	}
}

// recordRouteOutcome feeds a real request outcome back into the shared trust
// manager that also drives fabric pool scoring (routing.SelectCandidates reads
// trust levels from the same globalTrustManager instance). This is the piece
// that closes the telemetry -> health -> trust -> scoring -> routing feedback
// loop: without it, pool ordering would operate forever on the TrustUnknown
// baseline instead of reflecting real observed behavior. It is called from
// the combo-fallback loops in combo.go, so it covers every combo-shaped
// request — ordinary DB combos as well as fabric-free — since a pool always
// resolves through that same execution path.
//
// statusCode/errBody are only consulted when success is false; pass 0/nil for
// a plain non-HTTP transport error (timeout, connection reset, etc.), which
// classifies as a transient failure.
func recordRouteOutcome(provider, model, accountID string, success bool, statusCode int, errBody []byte) {
	if success {
		globalTrustManager.RecordObservation(provider, model, accountID, true, "")
		return
	}
	errText := extractErrorText(errBody)
	cls := providers.ClassifyError(statusCode, errText, 0)
	globalTrustManager.RecordObservation(provider, model, accountID, false, cls.Category)
}
