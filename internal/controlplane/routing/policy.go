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
}

// Engine evaluates and selects routing candidates based on policies.
type Engine struct {
	TrustManager *trust.Manager
}

// SelectCandidates evaluates a requested model/pool against the active registry snapshot,
// applying the specified routing policy, and returns a sorted list of fallback candidate nodes.
//
// When requestedModel names a registered Fabric logical pool (see the pools
// package), candidates are expanded across every ProviderModel satisfying the
// pool's membership predicate, regardless of ModelID — this is what lets a
// pool like "fabric-free" span heterogeneous models/providers instead of a
// single fixed upstream model. Otherwise it falls back to the historical
// behavior of an exact ModelID match, so ordinary explicit-model routing is
// unaffected.
func (e *Engine) SelectCandidates(requestedModel string, policy Policy) []RouteNode {
	state := registry.GetActiveState()
	if state == nil {
		return nil
	}

	pool, isPool := pools.Get(requestedModel)

	var candidates []RouteNode

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

			// Basic filtering: either the requested model is a logical pool
			// (membership decided by the pool predicate) or an exact ModelID
			// match (legacy direct routing).
			if isPool {
				if !pool.Member(pm) {
					continue
				}
			} else if pm.ModelID != requestedModel {
				continue
			}

			// Filter out inactive models
			if !pm.IsActive {
				continue
			}

			// Enforce Policy constraints
			isFree := (pm.PricingMode == "free" || pm.PricingMode == "free_tier")
			if (policy == PolicyFreeOnly && !isFree) || (isPool && !isFree) {
				// Pools currently only expand free-tier membership, so this is
				// redundant with the predicate above for fabric-free, but kept
				// explicit so a future non-free pool can't accidentally leak a
				// paid route through a mismatched policy argument.
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

				// Real observed success rate wins over a placeholder: an
				// unproven node gets an optimistic neutral prior so it still
				// gets a fair shot, but once it has a track record, scoring
				// reflects actual behavior instead of a static zero.
				successRate := trust.NeutralSuccessRate
				if sr, hasData := e.TrustManager.SuccessRate(provID, pm.ModelID, acc.ID); hasData {
					successRate = sr
				}

				factors := scoring.Factors{
					TrustLevel:         trustLvl,
					IsFreeTier:         isFree,
					AccountRiskPenalty: riskProfile.ScorePenalty,
					SuccessRate:        successRate,
					// TTFT/Latency are populated by probes when available;
					// left neutral here (see scoring.Calculate) until the
					// probe -> scoring wiring lands.
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
