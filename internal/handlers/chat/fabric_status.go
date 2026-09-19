package chat

import (
	"net/http"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/handlerutil"
)

type fabricCandidateStatus struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	AccountID  string  `json:"accountId,omitempty"`
	Eligible   bool    `json:"eligible"`
	SkipReason string  `json:"skipReason,omitempty"`
	Score      float64 `json:"score,omitempty"`
	TrustLevel string  `json:"trustLevel,omitempty"`
	Locked     bool    `json:"locked,omitempty"`
}

// HandleFabricStatus reports the live free pool, including skipped routes.
// GET /admin/fabric/status?pool=fabric-free
func (h *ChatHandler) HandleFabricStatus(w http.ResponseWriter, r *http.Request) {
	pool := r.URL.Query().Get("pool")
	if pool == "" {
		pool = pools.FabricFree
	}
	if !pools.IsPool(pool) {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "unknown fabric pool: "+pool)
		return
	}
	canon := pools.CanonicalName(pool)

	if h.Repo != nil {
		if db := h.Repo.RawDB(); db != nil {
			_ = cpsync.SyncAccountsFromDB(db)
		}
	}

	state := registry.GetActiveState()
	eligible := globalRoutingEngine.SelectCandidates(canon, routing.PolicyFreeOnly)

	eligibleSet := make(map[string]bool, len(eligible))
	var candidates []fabricCandidateStatus
	for _, n := range eligible {
		eligibleSet[n.ProviderID+"|"+n.ModelID] = true
		status := fabricCandidateStatus{
			Provider:   n.ProviderID,
			Model:      n.ModelID,
			AccountID:  n.AccountID,
			Eligible:   true,
			Score:      n.Score.Total,
			TrustLevel: string(globalTrustManager.GetTrustLevel(n.ProviderID, n.ModelID, n.AccountID)),
		}
		if h.Repo != nil {
			if locked, _ := h.Repo.IsConnectionModelLocked(n.AccountID, n.ModelID); locked {
				status.Locked = true
			}
		}
		candidates = append(candidates, status)
	}

	if state != nil {
		poolDef, _ := pools.Get(canon)
		for provID, models := range state.ProviderModels {
			p, ok := state.Providers[provID]
			for _, pm := range models {
				if pm == nil || !poolDef.Member(pm) {
					continue
				}
				if eligibleSet[provID+"|"+pm.ModelID] {
					continue
				}
				reason := "not eligible"
				switch {
				case !ok || p == nil || !p.IsActive:
					reason = "provider inactive"
				case !pm.IsActive:
					reason = "route deactivated (stale or disabled)"
				default:
					if !hasActiveAccount(state, provID) {
						reason = "no active account/credentials"
					} else if blocked, why := globalTrustManager.IsUnavailable(provID, pm.ModelID, ""); blocked {
						reason = why
					} else if lvl := globalTrustManager.GetTrustLevel(provID, pm.ModelID, ""); lvl == trustLevelQuarantined {
						reason = "quarantined"
					}
				}
				candidates = append(candidates, fabricCandidateStatus{
					Provider:   provID,
					Model:      pm.ModelID,
					Eligible:   false,
					SkipReason: reason,
				})
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"pool":       canon,
		"aliases":    pools.Names(),
		"enabled":    true,
		"candidates": candidates,
	})
}

const trustLevelQuarantined = "quarantined"

func hasActiveAccount(state *registry.RegistryState, providerID string) bool {
	for _, acc := range state.Accounts {
		if acc != nil && acc.ProviderID == providerID && acc.IsActive {
			return true
		}
	}
	return false
}
