package routing

import (
	"encoding/json"
	"sort"
	"strings"

	"9router/proxy/internal/controlplane/registry"
)

// FreeProfiles is the advertised set of built-in fail-closed routes. A paid
// fallback must be an explicit operator combo with selected models/accounts.
func FreeProfiles() []string {
	return []string{"free", "free-best", "coding-best-free", "reasoning-free", "fast-free", "long-context", "local"}
}

func IsFreeProfile(name string) bool {
	for _, profile := range FreeProfiles() {
		if profile == strings.ToLower(strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// FilterProfile uses declared capabilities, never model-name guesses or
// fabricated benchmarks. coding-best-free requires tool use; quality is
// ranked by the existing scoring engine when measured evidence is available.
func FilterProfile(state *registry.RegistryState, nodes []RouteNode, name string) []RouteNode {
	name = strings.ToLower(strings.TrimSpace(name))
	out := make([]RouteNode, 0, len(nodes))
	if state == nil || !IsFreeProfile(name) {
		return out
	}
	for _, node := range nodes {
		pm := state.ProviderModels[node.ProviderID][node.ModelID]
		if pm == nil || !IsFreePricing(pm.PricingMode) {
			continue
		}
		var caps struct {
			Tools     bool `json:"tools"`
			Reasoning bool `json:"reasoning"`
			Local     bool `json:"local"`
			Context   int  `json:"context_window"`
		}
		_ = json.Unmarshal([]byte(pm.Capabilities), &caps)
		switch name {
		case "coding-best-free":
			if !caps.Tools {
				continue
			}
		case "reasoning-free":
			if !caps.Reasoning {
				continue
			}
		case "long-context":
			if caps.Context < 128000 {
				continue
			}
		case "local":
			if !caps.Local || node.ProviderID != "llamacpp" {
				continue
			}
		}
		out = append(out, node)
	}
	if name == "fast-free" {
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i].LatencyMs, out[j].LatencyMs
			if a == 0 {
				return false
			}
			if b == 0 {
				return true
			}
			return a < b
		})
	}
	return out
}
