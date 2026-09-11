package chat

import (
	"net/http"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	cpsync "9router/proxy/internal/controlplane/sync"
	"9router/proxy/internal/handlerutil"
)

// fabricCandidateStatus is one candidate's full routing picture: score,
// current health/trust/cooldown state, and — for candidates skipped before
// scoring — why they didn't make it into the eligible list at all.
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

// HandleFabricStatus reports the live fabric-free pool: which candidates are
// currently eligible and in what order, their scores, and — for every free
// route in the registry, including ones that didn't make the cut — why they
// were skipped (no account, quarantined, locked/cooling down). Reuses the
// same registry/scoring/trust machinery the hot path uses, so this is always
// consistent with what an actual "fabric-free" request would do.
// GET /admin/fabric/status
func (h *ChatHandler) HandleFabricStatus(w http.ResponseWriter, r *http.Request) {
	pool := r.URL.Query().Get("pool")
	if pool == "" {
		pool = pools.FabricFree
	}
	if !pools.IsPool(pool) {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "unknown fabric pool: "+pool)
		return
	}

	if h.Repo != nil {
		if db := h.Repo.RawDB(); db != nil {
			_ = cpsync.SyncAccountsFromDB(db)
		}
	}

	state := registry.GetActiveState()
	eligible := globalRoutingEngine.SelectCandidates(pool, routing.PolicyFreeOnly)

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

	// Also surface every free route the registry knows about that did NOT
	// make the eligible list, with a best-effort reason, so "why was X
	// skipped" is answerable without cross-referencing multiple endpoints.
	if state != nil {
		poolDef, _ := pools.Get(pool)
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
					} else if lvl := globalTrustManager.GetTrustLevel(provID, pm.ModelID, ""); lvl == "quarantined" {
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
		"pool":       pool,
		"enabled":    true,
		"candidates": candidates,
	})
}

func hasActiveAccount(state *registry.RegistryState, providerID string) bool {
	for _, acc := range state.Accounts {
		if acc != nil && acc.ProviderID == providerID && acc.IsActive {
			return true
		}
	}
	return false
}
