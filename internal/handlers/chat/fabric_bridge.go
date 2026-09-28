package chat

import (
	"fmt"
	"strings"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
)

// resolveFabricPool expands fabric-free / free-best / free into a combo-shaped
// ModelInfo. The combo fallback path then owns account failover, locking, and
// streaming. Returns nil when no currently usable free route exists.
func (h *ChatHandler) resolveFabricPool(poolName string) *ModelInfo {
	canon := pools.CanonicalName(poolName)
	if canon == "" {
		return nil
	}
	if h.Repo != nil {
		if db := h.Repo.RawDB(); db != nil {
			if err := cpsync.SyncAccountsFromDB(db); err != nil {
				log.Error("fabric", "account sync failed, aborting pool resolution", "pool", canon, "error", err)
				return nil
			}
		}
	}

	nodes := globalRoutingEngine.SelectCandidates(canon, routing.PolicyFreeOnly)
	if len(nodes) == 0 {
		log.Warn("fabric", "no eligible candidates for pool", "pool", canon)
		return nil
	}

	seen := make(map[string]bool, len(nodes))
	entries := make([]string, 0, len(nodes))
	for _, n := range nodes {
		model := n.ModelID
		if model == "" {
			model = n.UpstreamModel
		}
		key := n.ProviderID + "/" + model
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, key)
	}
	if len(entries) == 0 {
		return nil
	}

	first := strings.SplitN(entries[0], "/", 2)
	log.Info("fabric", "pool resolved", "pool", canon, "requested", poolName, "candidates", len(entries), "top", entries[0])
	info := &ModelInfo{
		Provider:    first[0],
		Model:       first[1],
		ComboModels: entries,
		Strategy:    "fallback",
	}
	return info
}

func recordRouteOutcome(provider, model, accountID string, success bool, statusCode int, errBody []byte, latencyMs int) {
	if accountID == "" {
		return
	}
	if success {
		globalTrustManager.RecordObservation(provider, model, accountID, true, "")
		if latencyMs >= 0 {
			globalTrustManager.RecordLatency(provider, model, accountID, latencyMs)
		}
		return
	}
	errText := extractErrorText(errBody)
	cls := providers.ClassifyError(statusCode, errText, 0)
	globalTrustManager.RecordObservation(provider, model, accountID, false, cls.Category)
	if cls.Category == providers.ErrQuota || cls.Category == providers.ErrRateLimit {
		globalTrustManager.RecordQuota(provider, model, accountID, 0)
	}
	if cls.Category == providers.ErrSession {
		globalTrustManager.RecordSessionExpired(provider, model, accountID)
	}
}

func fabricNoRouteError(name string) error {
	return fmt.Errorf("%s: no currently usable free routes (verified registry is empty or all candidates are quarantined, quota-exhausted, expired, or missing credentials)", name)
}
