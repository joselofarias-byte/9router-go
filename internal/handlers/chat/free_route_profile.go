package chat

import (
	"strings"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
)

// retainDeclaredProfile drops a candidate only when discovery stored a
// capability document that contradicts the profile. Empty documents keep the
// existing provider-catalog ranking, so a profile cannot silently become paid
// and cannot discard models that never declared capabilities.
func retainDeclaredProfile(state *registry.RegistryState, models []string, profile virtualFreeProfile) []string {
	route := virtualFreeRouteName(profile)
	if state == nil || route == "" {
		return models
	}
	out := make([]string, 0, len(models))
	for _, entry := range models {
		pm := providerModelForFreeEntry(state, entry)
		if pm == nil {
			continue
		}
		node := routing.RouteNode{ProviderID: pm.ProviderID, ModelID: pm.ModelID}
		if len(routing.ApplyDeclaredProfile(state, []routing.RouteNode{node}, route)) == 0 {
			continue
		}
		out = append(out, nodeEntry(pm))
	}
	return out
}

func (h *ChatHandler) declaredProfileStillAllows(profile, entry string) bool {
	if profile == "" || profile == "free" || profile == "free-best" || profile == "fast-free" {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(profile))
	if name == "long-context" {
		name = "long-context-free"
	}
	var kind virtualFreeProfile
	switch name {
	case "reasoning-free":
		kind = virtualFreeProfileReasoning
	case "coding-best-free":
		kind = virtualFreeProfileCoding
	case "long-context-free":
		kind = virtualFreeProfileLongContext
	case "local":
		kind = virtualFreeProfileLocal
	default:
		return true
	}
	kept := retainDeclaredProfile(registry.GetActiveState(), []string{entry}, kind)
	return len(kept) == 1
}

func hasDeclaredCapabilities(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return trimmed != "" && trimmed != "{}" && trimmed != "null"
}

func providerModelForFreeEntry(state *registry.RegistryState, entry string) *registry.ProviderModel {
	parts := strings.SplitN(entry, "/", 2)
	if state == nil || len(parts) != 2 {
		return nil
	}
	for _, pm := range state.ProviderModels[parts[0]] {
		if pm == nil || !pm.IsActive {
			continue
		}
		if freeEntryWire(pm) == parts[1] {
			return pm
		}
	}
	return nil
}

func freeEntryWire(pm *registry.ProviderModel) string {
	if pm.UpstreamModel != "" {
		return pm.UpstreamModel
	}
	return pm.ModelID
}

func nodeEntry(pm *registry.ProviderModel) string {
	return pm.ProviderID + "/" + freeEntryWire(pm)
}

func virtualFreeRouteName(profile virtualFreeProfile) string {
	switch profile {
	case virtualFreeProfileFast:
		return "fast-free"
	case virtualFreeProfileReasoning:
		return "reasoning-free"
	case virtualFreeProfileCoding:
		return "coding-best-free"
	case virtualFreeProfileLongContext:
		return "long-context-free"
	case virtualFreeProfileLocal:
		return "local"
	default:
		return "free-best"
	}
}
