package routing

import (
	"math/rand"
	"sort"

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
}

// Engine evaluates and selects routing candidates based on policies.
type Engine struct {
	TrustManager *trust.Manager
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

			// Find accounts for this provider
			for _, acc := range state.Accounts {
				if acc == nil {
					continue
				}
				if acc.ProviderID != provID || !acc.IsActive {
					continue
				}

				trustLvl := e.TrustManager.GetTrustLevel(provID, pm.ModelID, acc.ID)

				if policy == PolicyTrusted && trustLvl != trust.TrustTrusted && trustLvl != trust.TrustVerified {
					continue
				}

				factors := scoring.Factors{
					TrustLevel:         trustLvl,
					IsFreeTier:         isFree,
					AccountRiskPenalty: riskProfile.ScorePenalty,
					// TTFT, Latency, etc., would be pulled from a metrics store
				}

				score := scoring.Calculate(factors)
				if !score.IsRoutable {
					continue
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
					AccountID:     acc.ID,
					Score:         score,
				})
			}
		}
	}

	// Pre-shuffle to distribute load among equally scored nodes
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	// Sort candidates by score descending (stable due to preceding shuffle)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score.Total > candidates[j].Score.Total
	})

	return candidates
}
