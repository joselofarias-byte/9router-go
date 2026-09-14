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
// An empty requestedModel means "all models allowed by policy". This is used by virtual
// routes such as free-best, where the policy itself defines the candidate pool.
func (e *Engine) SelectCandidates(requestedModel string, policy Policy) []RouteNode {
	state := registry.GetActiveState()
	if state == nil {
		return nil
	}

	var candidates []RouteNode
	for provID, providerModels := range state.ProviderModels {
		p, ok := state.Providers[provID]
		if !ok || p == nil || !p.IsActive {
			continue
		}

		for _, pm := range providerModels {
			if pm == nil || !pm.IsActive {
				continue
			}

			// Empty model means dynamic pool selection; otherwise preserve exact-match behavior.
			if requestedModel != "" && pm.ModelID != requestedModel {
				continue
			}

			isFree := pm.PricingMode == "free" || pm.PricingMode == "free_tier"
			if policy == PolicyFreeOnly && !isFree {
				continue
			}

			riskProfile := providers.GetProviderRiskProfile(provID)
			for _, acc := range state.Accounts {
				if acc == nil || acc.ProviderID != provID || !acc.IsActive {
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
				}

				score := scoring.Calculate(factors)
				if !score.IsRoutable {
					continue
				}
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

	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score.Total > candidates[j].Score.Total
	})

	return candidates
}
