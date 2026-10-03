package routing

import (
	"math/rand"
	"sort"

	"9router/proxy/internal/controlplane/availability"
	"9router/proxy/internal/controlplane/capacity"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/scoring"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/providers"
)

type Policy string

const (
	PolicyBalanced  Policy = "balanced"
	PolicyFreeOnly  Policy = "free-only"
	PolicyFreeFirst Policy = "free-first"
	PolicyTrusted   Policy = "trusted-only"
)

const virtualNoAuthAccountPrefix = "noauth:"

// VirtualNoAuthAccountID gives credential-free providers a stable routing
// identity for scoring without creating a fake database credential row.
func VirtualNoAuthAccountID(providerID string) string {
	return virtualNoAuthAccountPrefix + providerID
}

// IsVirtualNoAuthAccount validates the synthetic identity used only by
// KnownProviders entries explicitly marked NoAuth.
func IsVirtualNoAuthAccount(providerID, accountID string) bool {
	return providerID != "" && accountID == VirtualNoAuthAccountID(providerID)
}

// IsFreePricing reports whether Fabric currently classifies a model as free.
// The comparison is exact: discovery writes "free" and "free_tier", and any
// other spelling stays out of the free pool.
func IsFreePricing(mode string) bool {
	return mode == "free" || mode == "free_tier"
}

type RouteNode struct {
	ProviderID    string
	ModelID       string
	UpstreamModel string
	AccountID     string
	Score         scoring.Score
	LatencyMs     float64
	Quota         capacity.Snapshot
}

// Engine evaluates and selects routing candidates based on policies.
type Engine struct {
	TrustManager *trust.Manager
	Availability *availability.Store
}

// SelectCandidates evaluates a requested model/pool against the active registry snapshot,
// applying the specified routing policy, and returns a sorted list of fallback candidate nodes.
// An empty requestedModel selects every active model the policy allows. Virtual routes
// such as free and free-best use that form to build a dynamic pool.
func (e *Engine) SelectCandidates(requestedModel string, policy Policy) []RouteNode {
	state := registry.GetActiveState()
	if state == nil {
		return nil
	}

	var candidates []RouteNode

	// In a complete implementation, this would look up the routing pool definition
	// and expand requestedModel to all candidate models in the pool.
	// For now, we do a direct lookup of all ProviderModels matching the requested ID.
	for provID, providerModels := range state.ProviderModels {
		// Filter out inactive or missing providers entirely
		p, ok := state.Providers[provID]
		if !ok || p == nil || !p.IsActive {
			continue
		}

		for _, pm := range providerModels {
			// Defensively skip nil entries
			if pm == nil {
				continue
			}

			// Filter out inactive models
			if !pm.IsActive {
				continue
			}

			// Empty model means dynamic pool selection; otherwise preserve exact-match behavior.
			if requestedModel != "" && pm.ModelID != requestedModel {
				continue
			}

			// Enforce Policy constraints. Only the exact Fabric classifications
			// "free" and "free_tier" count. "FREE", "free ", "trial", empty,
			// paid, and unknown are not eligible for a free-only pool.
			isFree := IsFreePricing(pm.PricingMode)
			if policy == PolicyFreeOnly && !isFree {
				continue // strict drop
			}

			riskProfile := providers.GetProviderRiskProfile(provID)

			appendCandidate := func(accountID string) {
				observed, quota, ok := e.accountView(provID, accountID, pm)
				if !ok {
					return
				}
				trustLvl := e.TrustManager.GetTrustLevel(provID, pm.ModelID, accountID)

				if policy == PolicyTrusted && trustLvl != trust.TrustTrusted && trustLvl != trust.TrustVerified {
					return
				}

				factors := scoring.Factors{
					TrustLevel:         trustLvl,
					IsFreeTier:         isFree,
					AccountRiskPenalty: riskProfile.ScorePenalty,
					TTFTMs:             int(observed.TTFTMs),
				}
				if observed.Attempts > 0 {
					factors.SuccessRate = float64(observed.Successes) / float64(observed.Attempts)
				}

				score := scoring.Calculate(factors)
				if !score.IsRoutable {
					return
				}

				// If FreeFirst, strongly boost free models so they float to the top.
				// Account-risk penalties still order peers within the free tier.
				if policy == PolicyFreeFirst && isFree {
					score.Total += 1000.0
				}

				candidates = append(candidates, RouteNode{
					ProviderID:    provID,
					ModelID:       pm.ModelID,
					UpstreamModel: pm.UpstreamModel,
					AccountID:     accountID,
					Score:         score,
					LatencyMs:     observed.LatencyMs,
					Quota:         quota,
				})
			}

			hasActiveAccount := false
			for _, acc := range state.Accounts {
				if acc == nil || acc.ProviderID != provID || !acc.IsActive {
					continue
				}
				hasActiveAccount = true
				appendCandidate(acc.ID)
			}

			// Credential-free providers such as local llama.cpp are routable
			// without inventing a providerConnections row.
			if !hasActiveAccount {
				if cfg, known := providers.KnownProviders[provID]; known && cfg.NoAuth {
					appendCandidate(VirtualNoAuthAccountID(provID))
				}
			}
		}
	}

	// Pre-shuffle to distribute load among equally scored nodes
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	// Sort candidates by score descending (stable due to preceding shuffle).
	// A known positive balance only breaks a score tie inside one provider/model.
	sort.SliceStable(candidates, func(i, j int) bool {
		return preferCandidate(candidates[i], candidates[j])
	})

	return candidates
}

// accountView drops an account that is freshly exhausted or inside an observed
// cooldown. Unknown and stale quota stay eligible; they are not a balance.
func (e *Engine) accountView(providerID, accountID string, pm *registry.ProviderModel) (availability.State, capacity.Snapshot, bool) {
	wire := pm.UpstreamModel
	if wire == "" {
		wire = pm.ModelID
	}
	quota := capacity.Inspect(providerID, accountID, wire)
	if !capacity.Eligible(quota) {
		return availability.State{}, quota, false
	}
	var observed availability.State
	if e != nil && e.Availability != nil {
		observed = e.Availability.Get(availability.Key{Provider: providerID, Model: pm.ModelID, Account: accountID})
		if !observed.BlockedUntil.IsZero() {
			return observed, quota, false
		}
	}
	return observed, quota, true
}

func preferCandidate(a, b RouteNode) bool {
	if a.Score.Total != b.Score.Total {
		return a.Score.Total > b.Score.Total
	}
	aKnown := a.Quota.Status == capacity.Available
	bKnown := b.Quota.Status == capacity.Available
	if aKnown != bKnown {
		return aKnown
	}
	if !aKnown || a.ProviderID != b.ProviderID || a.ModelID != b.ModelID {
		return false
	}
	return a.Quota.RemainingPercentage > b.Quota.RemainingPercentage
}
