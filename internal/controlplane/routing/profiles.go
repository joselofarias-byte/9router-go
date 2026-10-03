package routing

import (
	"encoding/json"
	"sort"
	"strings"

	"9router/proxy/internal/controlplane/registry"
)

// FreeProfiles is the advertised set of built-in fail-closed routes.
// A paid fallback is an explicit operator combo, never an implicit profile.
func FreeProfiles() []string {
	return []string{
		"free",
		"free-best",
		"coding-best-free",
		"reasoning-free",
		"fast-free",
		"long-context-free",
		"local",
	}
}

func canonicalFreeProfile(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "long-context" {
		return "long-context-free"
	}
	return name
}

// IsFreeProfile reports whether name is a built-in fail-closed free route.
func IsFreeProfile(name string) bool {
	name = canonicalFreeProfile(name)
	for _, profile := range FreeProfiles() {
		if profile == name {
			return true
		}
	}
	return false
}

type declaredCaps struct {
	Tools     bool `json:"tools"`
	Reasoning bool `json:"reasoning"`
	Local     bool `json:"local"`
	Context   int  `json:"context_window"`
}

// FilterProfile keeps free-priced nodes that match a built-in profile.
// Capability checks use the JSON the discovery adapter stored. They are not
// model-name guesses. Unknown profiles, including any paid-fallback name,
// return an empty list.
func FilterProfile(state *registry.RegistryState, nodes []RouteNode, name string) []RouteNode {
	name = canonicalFreeProfile(name)
	out := make([]RouteNode, 0, len(nodes))
	if state == nil || !IsFreeProfile(name) {
		return out
	}
	for _, node := range nodes {
		pm := providerModel(state, node)
		if pm == nil || !IsFreePricing(pm.PricingMode) {
			continue
		}
		if !profileAllows(name, node, pm) {
			continue
		}
		out = append(out, node)
	}
	if name == "fast-free" {
		sort.SliceStable(out, func(i, j int) bool {
			return faster(out[i].LatencyMs, out[j].LatencyMs)
		})
	}
	return out
}

func providerModel(state *registry.RegistryState, node RouteNode) *registry.ProviderModel {
	models := state.ProviderModels[node.ProviderID]
	if models == nil {
		return nil
	}
	return models[node.ModelID]
}

func profileAllows(name string, node RouteNode, pm *registry.ProviderModel) bool {
	var caps declaredCaps
	_ = json.Unmarshal([]byte(pm.Capabilities), &caps)
	switch name {
	case "coding-best-free":
		return caps.Tools
	case "reasoning-free":
		return caps.Reasoning
	case "long-context-free":
		return caps.Context >= 128000
	case "local":
		return caps.Local && node.ProviderID == "llamacpp"
	default:
		return true
	}
}

func faster(a, b float64) bool {
	if a == 0 {
		return false
	}
	if b == 0 {
		return true
	}
	return a < b
}

// ApplyDeclaredProfile is the send-path gate. A model with no capability
// document stays eligible, except on the local profile. A document that
// contradicts the profile is removed. Paid and unknown profiles are empty.
func ApplyDeclaredProfile(state *registry.RegistryState, nodes []RouteNode, name string) []RouteNode {
	name = canonicalFreeProfile(name)
	if state == nil || !IsFreeProfile(name) {
		return nil
	}
	strict := name == "local"
	out := make([]RouteNode, 0, len(nodes))
	for _, node := range nodes {
		pm := providerModel(state, node)
		if pm == nil || !IsFreePricing(pm.PricingMode) {
			continue
		}
		if (strict || hasDeclaredCapabilities(pm.Capabilities)) && !profileAllows(name, node, pm) {
			continue
		}
		out = append(out, node)
	}
	return out
}

func hasDeclaredCapabilities(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return trimmed != "" && trimmed != "{}" && trimmed != "null"
}
