package chat

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"9router/proxy/internal/controlplane/pools"
	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/log"
	"9router/proxy/internal/models"
	"9router/proxy/internal/providers"
)

const FreeRouteUnavailableCode = "free_route_unavailable"

const freeRouteUnavailableMessage = "free route unavailable: no eligible free or free-tier models with an active local connection"

var ErrFreeRouteUnavailable = errors.New(FreeRouteUnavailableCode)

func freeRouteUnavailable(detail string) error {
	if detail == "" {
		return ErrFreeRouteUnavailable
	}
	return fmt.Errorf("%w: %s", ErrFreeRouteUnavailable, detail)
}

func writeFreeRouteUnavailable(w http.ResponseWriter) {
	handlerutil.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": map[string]any{
			"message": freeRouteUnavailableMessage,
			"type":    "server_error",
			"code":    FreeRouteUnavailableCode,
		},
	})
}

// WriteResolveError keeps ordinary resolver failures as 400 while exposing a
// stable 503 contract for the built-in free route. The fixed message never
// includes connection data or credentials.
func WriteResolveError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrFreeRouteUnavailable) {
		writeFreeRouteUnavailable(w)
		return
	}
	handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
}

func isVirtualFreeRoute(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case pools.Free, pools.FreeBest, pools.FabricFree:
		return true
	default:
		return false
	}
}

func canonicalVirtualName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// resolveDynamicFreeBest creates a transient fallback chain from the live
// registry. No candidate means fail closed: the caller must not continue into
// the generic openai/anthropic/deepseek fallback.
func (h *ChatHandler) resolveDynamicFreeBest() (*ModelInfo, error) {
	if h == nil || h.Repo == nil {
		return nil, freeRouteUnavailable("repository unavailable")
	}
	candidates := getPolicyCandidates(nil, h.Repo.RawDB(), "", routing.PolicyFreeOnly)
	if len(candidates) == 0 {
		return nil, freeRouteUnavailable("no discovered free routes")
	}

	entries := freeRouteEntries(registry.GetActiveState(), candidates)
	if len(entries) == 0 {
		return nil, freeRouteUnavailable("no connected free routes")
	}

	var first *ModelInfo
	resolved := make([]string, 0, len(entries))
	for _, entry := range entries {
		info := h.resolveModelEntry(entry)
		if info == nil {
			continue
		}
		resolved = append(resolved, entry)
		if first == nil {
			first = info
		}
	}
	if first == nil || len(resolved) == 0 {
		return nil, freeRouteUnavailable("free candidates could not be resolved")
	}

	first.ComboModels = resolved
	first.Strategy = "fallback"
	first.VirtualFree = true
	return first, nil
}

// freeEntryStillEligible re-reads account state, provider/model activity and
// free pricing immediately before a dynamic-pool hop.
func (h *ChatHandler) freeEntryStillEligible(entry string) bool {
	if h == nil || h.Repo == nil || entry == "" {
		return false
	}
	candidates := getPolicyCandidates(nil, h.Repo.RawDB(), "", routing.PolicyFreeOnly)
	for _, current := range freeRouteEntries(registry.GetActiveState(), candidates) {
		if current == entry {
			return true
		}
	}
	return false
}

func (h *ChatHandler) AllowVirtualFreeHop(virtualFree bool, entry string) bool {
	if !virtualFree {
		return true
	}
	if h.freeEntryStillEligible(entry) {
		return true
	}
	log.Warn("fabric", "free hop no longer eligible; skipping", "entry", entry)
	return false
}

// SelectVirtualFreeEntry is used by endpoints that cannot execute a combo.
// It returns the first candidate that remains free at forwarding time.
func (h *ChatHandler) SelectVirtualFreeEntry(info *ModelInfo) (*ModelInfo, error) {
	if info == nil || !info.VirtualFree {
		return info, nil
	}
	for _, entry := range info.ComboModels {
		if !h.freeEntryStillEligible(entry) {
			continue
		}
		picked := h.resolveModelEntry(entry)
		if picked == nil {
			continue
		}
		picked.VirtualFree = true
		picked.Strategy = info.Strategy
		picked.ComboModels = info.ComboModels
		return picked, nil
	}
	return nil, freeRouteUnavailable("all free candidates became ineligible")
}

func freeRouteEntries(state *registry.RegistryState, candidates []routing.RouteNode) []string {
	if state == nil {
		return nil
	}
	seen := make(map[string]bool, len(candidates))
	entries := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		pm, ok := eligibleFreeProviderModel(state, candidate)
		if !ok {
			continue
		}
		model := pm.UpstreamModel
		if model == "" {
			model = pm.ModelID
		}
		if candidate.ProviderID == "" || model == "" {
			continue
		}
		entry := candidate.ProviderID + "/" + model
		if seen[entry] {
			continue
		}
		seen[entry] = true
		entries = append(entries, entry)
	}
	return entries
}

func eligibleFreeProviderModel(state *registry.RegistryState, candidate routing.RouteNode) (*registry.ProviderModel, bool) {
	if state == nil || candidate.ProviderID == "" || candidate.ModelID == "" || candidate.AccountID == "" {
		return nil, false
	}
	provider := state.Providers[candidate.ProviderID]
	if provider == nil || !provider.IsActive {
		return nil, false
	}
	account := state.Accounts[candidate.AccountID]
	if account == nil || !account.IsActive || account.ProviderID != candidate.ProviderID {
		return nil, false
	}
	pm := providerModelByID(state.ProviderModels[candidate.ProviderID], candidate.ModelID)
	if pm == nil || !pm.IsActive || pm.ProviderID != candidate.ProviderID || !pools.IsFreePricing(pm) {
		return nil, false
	}
	return pm, true
}

func providerModelByID(models map[string]*registry.ProviderModel, modelID string) *registry.ProviderModel {
	if models == nil || modelID == "" {
		return nil
	}
	if pm := models[modelID]; pm != nil && pm.ModelID == modelID {
		return pm
	}
	for _, pm := range models {
		if pm != nil && pm.ModelID == modelID {
			return pm
		}
	}
	return nil
}

// resolveFoldedVirtualOverride preserves caller-defined aliases/combos named
// free/free-best even if the request differs only in case or surrounding space.
func (h *ChatHandler) resolveFoldedVirtualOverride(request string) (*ModelInfo, error, bool) {
	if h == nil || h.Repo == nil {
		return nil, nil, false
	}
	canonical := canonicalVirtualName(request)
	if canonical != pools.Free && canonical != pools.FreeBest && canonical != pools.FabricFree {
		return nil, nil, false
	}

	if aliases, err := h.Repo.GetModelAliases(); err == nil {
		for key, target := range aliases {
			name := strings.TrimSpace(string(key))
			if !strings.EqualFold(name, canonical) || !strings.Contains(target, "/") {
				continue
			}
			parts := strings.SplitN(target, "/", 2)
			provider := resolveProviderAlias(parts[0])
			if info := h.resolvePrefixProvider(parts[0], parts[1]); info != nil {
				return info, nil, true
			}
			return &ModelInfo{Provider: provider, Model: parts[1]}, nil, true
		}
	}

	combos, err := h.Repo.GetCombos()
	if err != nil {
		return nil, nil, false
	}
	for _, combo := range combos {
		if combo == nil || !strings.EqualFold(strings.TrimSpace(combo.Name), canonical) {
			continue
		}
		info, comboErr := h.resolveExplicitCombo(combo)
		return info, comboErr, true
	}
	return nil, nil, false
}

func (h *ChatHandler) resolveExplicitCombo(combo *models.Combo) (*ModelInfo, error) {
	if combo == nil || combo.Models == "" {
		return nil, fmt.Errorf("combo is empty")
	}
	var raw []string
	if err := json.Unmarshal([]byte(combo.Models), &raw); err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("combo %s has no valid models", combo.Name)
	}
	flattened, err := h.flattenComboModels(raw)
	if err != nil {
		return nil, err
	}
	if len(flattened) == 0 {
		return nil, fmt.Errorf("combo %s has no valid models", combo.Name)
	}
	first := h.resolveModelEntry(flattened[0])
	if first == nil {
		first, _ = h.resolveModel(flattened[0])
	}
	if first == nil {
		return nil, fmt.Errorf("combo %s could not resolve its first model", combo.Name)
	}
	first.ComboModels = flattened
	strategy, sticky, judge := h.resolveComboRouting(combo.Name, combo.Strategy)
	first.Strategy = strategy
	first.StickyLimit = sticky
	first.JudgeModel = judge
	return first, nil
}

// concreteVirtualOverride is deliberately conservative: it only recognizes
// explicit alias/combo definitions. Built-in provider names remain untouched.
func (h *ChatHandler) concreteVirtualOverride(request string) (*ModelInfo, error, bool) {
	if !isVirtualFreeRoute(request) {
		return nil, nil, false
	}
	return h.resolveFoldedVirtualOverride(request)
}

// Keep providers imported here intentionally: this compile-time reference
// catches accidental removal of provider alias support used by overrides.
var _ = providers.ProviderAliasMap
