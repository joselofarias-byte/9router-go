package routing

import (
	"math/rand"
	"sort"

	"9router/proxy/internal/controlplane/pools"
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
	SkipReason    string
}

// Engine evaluates and selects routing candidates based on policies.
type Engine struct {
	TrustManager *trust.Manager
}

// SelectCandidates evaluates a requested model/pool against the active
// registry snapshot and returns scored, sorted fallback candidates.
//
// requestedModel may be:
//   - a Fabric pool name (fabric-free / free-best / free): expand every
//     ProviderModel matching the pool predicate
//   - empty: expand every model allowed by policy (used by tests and
//     policy-aware callers)
//   - a concrete ModelID: exact match only
func (e *Engine) SelectCandidates(requestedModel string, policy Policy) []RouteNode {
	state := registry.GetActiveState()
	if state == nil || e == nil || e.TrustManager == nil {
		return nil
	}

	pool, isPool := pools.Get(requestedModel)
	dynamicAll := requestedModel == ""

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

			switch {
			case isPool:
				if !pool.Member(pm) {
					continue
				}
			case dynamicAll:
				// policy decides membership
			default:
				if pm.ModelID != requestedModel {
					continue
				}
			}

			isFree := pools.IsFreePricing(pm)
			if policy == PolicyFreeOnly && !isFree {
				continue
			}
			if isPool && !isFree {
				continue
			}

			riskProfile := providers.GetProviderRiskProfile(provID)

			for _, acc := range state.Accounts {
				if acc == nil || acc.ProviderID != provID || !acc.IsActive {
					continue
				}

				if blocked, _ := e.TrustManager.IsUnavailable(provID, pm.ModelID, acc.ID); blocked {
					continue
				}

				trustLvl := e.TrustManager.GetTrustLevel(provID, pm.ModelID, acc.ID)
				if policy == PolicyTrusted && trustLvl != trust.TrustTrusted && trustLvl != trust.TrustVerified {
					continue
				}

				successRate := trust.NeutralSuccessRate
				if sr, hasData := e.TrustManager.SuccessRate(provID, pm.ModelID, acc.ID); hasData {
					successRate = sr
				}

				factors := scoring.Factors{
					TrustLevel:         trustLvl,
					IsFreeTier:         isFree,
					AccountRiskPenalty: riskProfile.ScorePenalty,
					SuccessRate:        successRate,
				}
				if avgMs, hasLatency := e.TrustManager.LatencyStats(provID, pm.ModelID, acc.ID); hasLatency {
					factors.TTFTMs = avgMs
					factors.LatencyMs = avgMs
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
