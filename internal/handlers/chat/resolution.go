package chat

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlers/shared"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/log"
	"9router/proxy/internal/models"
	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy/executor"
	"9router/proxy/internal/proxy/oauth"
)

// FreeRouteUnavailableCode is the OpenAI-style error code clients match when
// the virtual free / free-best pool has no eligible model. The request fails
// closed: it is never sent to a paid provider under that name.
const FreeRouteUnavailableCode = "free_route_unavailable"

const freeRouteUnavailableMessage = "free route unavailable: no eligible free or free-tier models with an active local connection"

// ErrFreeRouteUnavailable is the sentinel for an empty or stale virtual free pool.
// errors.Is matches it, including errors wrapped with a detail suffix.
var ErrFreeRouteUnavailable = errors.New(FreeRouteUnavailableCode)

func freeRouteUnavailable(detail string) error {
	if detail == "" {
		return ErrFreeRouteUnavailable
	}
	return fmt.Errorf("%w: %s", ErrFreeRouteUnavailable, detail)
}

// writeFreeRouteUnavailable writes the stable OpenAI-style 503 body.
// The message is fixed so a client never receives connection secrets.
func writeFreeRouteUnavailable(w http.ResponseWriter) {
	handlerutil.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": map[string]any{
			"message": freeRouteUnavailableMessage,
			"type":    "server_error",
			"code":    FreeRouteUnavailableCode,
		},
	})
}

// WriteResolveError maps a resolveModel failure to an OpenAI-style JSON error.
// An unavailable virtual free pool is HTTP 503 with code free_route_unavailable.
// Every other resolve failure stays HTTP 400.
func WriteResolveError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrFreeRouteUnavailable) {
		writeFreeRouteUnavailable(w)
		return
	}
	handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
}

// NewChatHandler creates a ChatHandler with the given repository and a streaming-capable HTTP client.
// Pass a TokenSaverConfig to enable token saver features, or nil for all-off defaults.
func NewChatHandler(repo *db.Repo, ts ...*shared.TokenSaverConfig) *ChatHandler {
	executor.RegisterAll()
	oauth.RegisterAll()
	cfg := &shared.TokenSaverConfig{}
	if len(ts) > 0 && ts[0] != nil {
		cfg = ts[0]
	}
	// Timeout: 0 is required so long SSE streams are not cut short, but a
	// ResponseHeaderTimeout bounds how long we wait for the upstream to
	// start responding — closing the "accept then go silent" gap without
	// killing a stream that has already begun.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	return &ChatHandler{
		Repo: repo,
		Client: &http.Client{
			Transport: transport,
			Timeout:   0, // no timeout for streaming support
		},
		TokenSaver:  cfg,
		stickyState: make(map[string]*comboStickyState),
	}
}

// ResolveModel resolves a model string through aliases, combos, and provider/model parsing.
// Exported so other handlers (media, responses, etc.) can resolve model names.
func (h *ChatHandler) ResolveModel(modelStr string) (*ModelInfo, error) {
	return h.resolveModel(modelStr)
}

// resolveProviderAlias resolves a provider alias to its canonical ID.
func resolveProviderAlias(alias string) string {
	if canonical, ok := providers.ProviderAliasMap[alias]; ok {
		return canonical
	}
	return alias
}

// resolveModelEntry parses a single "provider/model" string into a ModelInfo
// without combo or alias resolution (used when iterating combo entries).
// If the entry has no "/" (i.e. it's a combo name), it resolves the combo
// and returns its first concrete model with the combined model list.
func (h *ChatHandler) resolveModelEntry(entry string) *ModelInfo {
	if !strings.Contains(entry, "/") {
		combo, err := h.Repo.GetComboByName(entry)
		if err == nil && combo != nil && combo.Models != "" {
			var subModels []string
			if err := json.Unmarshal([]byte(combo.Models), &subModels); err == nil && len(subModels) > 0 {
				first := h.resolveModelEntry(subModels[0])
				if first != nil {
					first.ComboModels = subModels
					first.Strategy = combo.Strategy
					return first
				}
			}
		}
		return nil
	}
	parts := strings.SplitN(entry, "/", 2)
	provider := resolveProviderAlias(parts[0])
	if _, ok := providers.KnownProviders[provider]; !ok {
		if info := h.resolvePrefixProvider(provider, parts[1]); info != nil {
			return info
		}
	}
	return &ModelInfo{Provider: provider, Model: parts[1]}
}

// flattenComboModels recursively expands combo-name entries into concrete
// "provider/model" leaves, keeping order and deduping consecutive identical
// leaves so a nested combo can't create pointless rotation slots. Guards
// against cyclic combo references by skipping recursive cycles. Inner-combo
// strategies are not applied here; the top-level combo's strategy governs
// the flattened list.
func (h *ChatHandler) flattenComboModels(models []string) ([]string, error) {
	out := make([]string, 0, len(models))
	seen := make(map[string]bool)
	var walk func([]string) error
	walk = func(ms []string) error {
		for _, m := range ms {
			if !strings.Contains(m, "/") {
				if seen[m] {
					log.Warn("combo", "cyclic combo reference detected, skipping", "combo", m)
					continue
				}
				if combo, err := h.Repo.GetComboByName(m); err == nil && combo != nil && combo.Models != "" {
					var sub []string
					if err := json.Unmarshal([]byte(combo.Models), &sub); err == nil {
						seen[m] = true
						if err := walk(sub); err != nil {
							return err
						}
						delete(seen, m)
						continue
					}
				}
				if aliasTarget, err := h.Repo.GetModelAlias(m); err == nil && aliasTarget != "" && strings.Contains(aliasTarget, "/") {
					m = aliasTarget
				}
			}
			if len(out) == 0 || out[len(out)-1] != m {
				out = append(out, m)
			}
		}
		return nil
	}
	if err := walk(models); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("combo has no valid leaf models")
	}
	return out, nil
}

// stripModelContextMarker strips trailing [1m] marker that Claude Code appends for 1M context beta.
// Port of decolua/9router PR #3691 (open-sse/utils/modelMarkers.js).
// Claude Code sends model: "claude-opus-5[1m]" — the marker is client-side annotation, not a real model.
// It must be stripped before combo/alias/provider lookup, while anthropic-beta header still carries the capability.
func stripModelContextMarker(modelStr string) string {
	trimmed := strings.TrimSpace(modelStr)
	if len(trimmed) < 4 {
		return modelStr
	}
	// Case-insensitive check for trailing "[1m]"
	suffix := trimmed[len(trimmed)-4:]
	if strings.EqualFold(suffix, "[1m]") {
		// Only strip if it's a trailing marker, not bracket inside name
		return strings.TrimSpace(trimmed[:len(trimmed)-4])
	}
	return modelStr
}

// isVirtualFreeRoute reports the built-in names. free and free-best are the
// same dynamic pool: score order puts the current best candidate first, and
// both names then fall back through the rest of that free-only chain.
// Matching trims space and ignores case.
func isVirtualFreeRoute(modelStr string) bool {
	switch canonicalVirtualName(modelStr) {
	case "free", "free-best":
		return true
	default:
		return false
	}
}

func canonicalVirtualName(modelStr string) string {
	return strings.ToLower(strings.TrimSpace(modelStr))
}

// resolveDynamicFreeBest builds a transient fallback combo from discovered
// free and free-tier models that also have an active local provider connection.
// An explicit alias or combo named free / free-best is resolved earlier and wins.
// The error return is fail-closed: callers must not continue into the
// openai/anthropic/deepseek fallback with the virtual name.
func (h *ChatHandler) resolveDynamicFreeBest() (*ModelInfo, error) {
	candidates := getPolicyCandidates(nil, h.Repo.RawDB(), "", routing.PolicyFreeOnly)
	if len(candidates) == 0 {
		return nil, freeRouteUnavailable("no discovered free or free-tier models with an active local connection")
	}

	// Second gate. SelectCandidates already drops non-free rows; this re-reads
	// the current registry so a paid, unknown, inactive, or disconnected model
	// cannot enter the chain if the policy filter or a stale candidate is wrong.
	state := registry.GetActiveState()
	models := freeRouteEntries(state, candidates)
	if len(models) == 0 {
		return nil, freeRouteUnavailable("no discovered free or free-tier models with an active local connection")
	}

	var first *ModelInfo
	resolved := make([]string, 0, len(models))
	for _, entry := range models {
		info := h.resolveModelEntry(entry)
		if info == nil {
			continue
		}
		resolved = append(resolved, entry)
		if first == nil {
			first = info
		}
	}
	if first == nil {
		return nil, freeRouteUnavailable("failed to resolve free candidates")
	}
	first.ComboModels = resolved
	first.Strategy = "fallback"
	first.VirtualFree = true
	return first, nil
}

// freeEntryStillEligible re-reads Fabric pricing and the active local account
// for one dynamic-pool entry. A sync failure or a paid, inactive, or
// disconnected model is not eligible.
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

// AllowVirtualFreeHop reports whether this fallback hop may run.
// Explicit user combos pass virtualFree false and are never filtered, so a
// caller-defined free or free-best combo can still include paid models.
func (h *ChatHandler) AllowVirtualFreeHop(virtualFree bool, entry string) bool {
	if !virtualFree {
		return true
	}
	if h.freeEntryStillEligible(entry) {
		return true
	}
	log.Warn("combo", "skip free hop no longer eligible", "entry", entry)
	return false
}

// SelectVirtualFreeEntry returns the first dynamic-pool entry that is still
// free and connected. Single-target endpoints use it before they forward.
func (h *ChatHandler) SelectVirtualFreeEntry(info *ModelInfo) (*ModelInfo, error) {
	if info == nil || !info.VirtualFree {
		return info, nil
	}
	for _, entry := range info.ComboModels {
		if !h.freeEntryStillEligible(entry) {
			log.Warn("combo", "skip free hop no longer eligible", "entry", entry)
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
	return nil, freeRouteUnavailable("no eligible free or free-tier models with an active local connection")
}

// freeRouteEntries returns provider/upstream pairs that are still free and
// backed by an active provider plus an active local account in state.
func freeRouteEntries(state *registry.RegistryState, candidates []routing.RouteNode) []string {
	if state == nil {
		return nil
	}
	models := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		pm, ok := eligibleFreeProviderModel(state, c)
		if !ok {
			continue
		}
		model := pm.UpstreamModel
		if model == "" {
			model = pm.ModelID
		}
		if c.ProviderID == "" || model == "" {
			continue
		}
		entry := c.ProviderID + "/" + model
		if seen[entry] {
			continue
		}
		seen[entry] = true
		models = append(models, entry)
	}
	return models
}

func eligibleFreeProviderModel(state *registry.RegistryState, c routing.RouteNode) (*registry.ProviderModel, bool) {
	if state == nil || c.ProviderID == "" || c.ModelID == "" || c.AccountID == "" {
		return nil, false
	}
	provider := state.Providers[c.ProviderID]
	if provider == nil || !provider.IsActive {
		return nil, false
	}
	account := state.Accounts[c.AccountID]
	if account == nil || !account.IsActive || account.ProviderID != c.ProviderID {
		return nil, false
	}
	pm := providerModelByID(state.ProviderModels[c.ProviderID], c.ModelID)
	if pm == nil || !pm.IsActive || pm.ProviderID != c.ProviderID || !routing.IsFreePricing(pm.PricingMode) {
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

// resolveComboModel expands a stored combo. ok is false when the row is not a
// usable combo so the caller can keep walking. A flatten error is returned
// with ok true so a broken explicit combo is not replaced by another route.
func (h *ChatHandler) resolveComboModel(combo *models.Combo) (*ModelInfo, error, bool) {
	if combo == nil || combo.Models == "" {
		return nil, nil, false
	}
	var modelStrings []string
	if err := json.Unmarshal([]byte(combo.Models), &modelStrings); err != nil || len(modelStrings) == 0 {
		return nil, nil, false
	}
	// Flatten nested combos into concrete leaves so rotation covers
	// every reachable model (a nested combo entry used to collapse to
	// its first leaf, so combo-wombo -> free-tier never rotated).
	flattened, flatErr := h.flattenComboModels(modelStrings)
	if flatErr != nil {
		return nil, flatErr, true
	}
	if len(flattened) == 0 {
		return nil, nil, false
	}
	firstInfo := h.resolveModelEntry(flattened[0])
	if firstInfo == nil {
		firstInfo, _ = h.resolveModel(flattened[0])
	}
	if firstInfo == nil {
		return nil, nil, false
	}
	firstInfo.ComboModels = flattened
	firstInfo.Strategy = combo.Strategy
	return firstInfo, nil, true
}

func (h *ChatHandler) concreteAliasTarget(target string) *ModelInfo {
	if !strings.Contains(target, "/") {
		return nil
	}
	parts := strings.SplitN(target, "/", 2)
	provider := resolveProviderAlias(parts[0])
	if _, ok := providers.KnownProviders[provider]; !ok {
		if info := h.resolvePrefixProvider(provider, parts[1]); info != nil {
			return info
		}
	}
	return &ModelInfo{Provider: provider, Model: parts[1]}
}

// resolveFoldedVirtualOverride matches an alias or combo whose name differs
// from the request only by case or surrounding space. Exact-case lookups have
// already run. Alias wins over combo, matching the exact-case order.
func (h *ChatHandler) resolveFoldedVirtualOverride(modelStr string) (*ModelInfo, error, bool) {
	canonical := canonicalVirtualName(modelStr)
	if canonical != "free" && canonical != "free-best" {
		return nil, nil, false
	}

	if aliases, err := h.Repo.GetModelAliases(); err == nil {
		target, ok := foldedAliasTarget(aliases, canonical)
		if ok {
			if info := h.concreteAliasTarget(target); info != nil {
				return info, nil, true
			}
		}
	}

	combos, err := h.Repo.GetCombos()
	if err != nil {
		return nil, nil, false
	}
	matched := foldedComboName(combos, canonical)
	if matched == "" || matched == modelStr {
		return nil, nil, false
	}
	combo, err := h.Repo.GetComboByName(matched)
	if err != nil {
		return nil, err, true
	}
	return h.resolveComboModel(combo)
}

func foldedAliasTarget(aliases map[string]string, canonical string) (string, bool) {
	var target string
	found := false
	for key, value := range aliases {
		if !strings.EqualFold(strings.TrimSpace(key), canonical) {
			continue
		}
		if strings.TrimSpace(key) == canonical {
			return value, true
		}
		if !found {
			target = value
			found = true
		}
	}
	return target, found
}

func foldedComboName(combos []*models.Combo, canonical string) string {
	matched := ""
	for _, combo := range combos {
		if combo == nil {
			continue
		}
		name := strings.TrimSpace(combo.Name)
		if !strings.EqualFold(name, canonical) {
			continue
		}
		if name == canonical {
			return combo.Name
		}
		if matched == "" {
			matched = combo.Name
		}
	}
	return matched
}

// resolveModel resolves a model string through aliases, combos, and provider/model parsing.
// Returns the first concrete ModelInfo found, or an error.
func (h *ChatHandler) resolveModel(modelStr string) (*ModelInfo, error) {
	if modelStr == "" {
		return nil, fmt.Errorf("missing model")
	}
	// Strip [1m] context marker before resolution (PR #3691)
	modelStr = stripModelContextMarker(modelStr)

	// 1. Standard format: "provider/model"
	if strings.Contains(modelStr, "/") {
		parts := strings.SplitN(modelStr, "/", 2)
		providerAlias := parts[0]
		model := parts[1]
		provider := resolveProviderAlias(providerAlias)

		if _, ok := providers.KnownProviders[provider]; !ok {
			if info := h.resolvePrefixProvider(provider, model); info != nil {
				return info, nil
			}
		}
		return &ModelInfo{Provider: provider, Model: model}, nil
	}

	// 2. Check if it's a model alias (e.g., "gpt-4o" -> "openai/gpt-4o")
	aliasTarget, err := h.Repo.GetModelAlias(modelStr)
	if err == nil && aliasTarget != "" {
		if strings.Contains(aliasTarget, "/") {
			parts := strings.SplitN(aliasTarget, "/", 2)
			provider := resolveProviderAlias(parts[0])
			if _, ok := providers.KnownProviders[provider]; !ok {
				if info := h.resolvePrefixProvider(provider, parts[1]); info != nil {
					return info, nil
				}
			}
			return &ModelInfo{
				Provider: provider,
				Model:    parts[1],
			}, nil
		}
	}

	// 3. Check if it's a combo name
	combo, err := h.Repo.GetComboByName(modelStr)
	if err == nil {
		if info, comboErr, ok := h.resolveComboModel(combo); ok {
			return info, comboErr
		}
	}

	// Built-in virtual route. Checked after aliases and combos so a caller-defined
	// "free" or "free-best" pool keeps working, and before the common-provider
	// fallback so the name cannot be sent to a paid provider by accident.
	// Case and surrounding space fold onto the same route: FREE and " free-best "
	// are the virtual names, and an alias or combo stored under any case of
	// those names still wins.
	if isVirtualFreeRoute(modelStr) {
		if info, overrideErr, ok := h.resolveFoldedVirtualOverride(modelStr); ok {
			return info, overrideErr
		}
		return h.resolveDynamicFreeBest()
	}

	// 3.5 Check if it's a bare provider alias (e.g., "ag" -> "antigravity")
	// Next.js treats bare alias as provider with default model for search/media endpoints.
	if canonical := resolveProviderAlias(modelStr); canonical != modelStr {
		if _, ok := providers.KnownProviders[canonical]; ok {
			if conns, err := h.Repo.GetProviderConnections(canonical, true); err == nil && len(conns) > 0 {
				return &ModelInfo{Provider: canonical, Model: ""}, nil
			}
		}
	}
	if _, ok := providers.KnownProviders[modelStr]; ok {
		if conns, err := h.Repo.GetProviderConnections(modelStr, true); err == nil && len(conns) > 0 {
			return &ModelInfo{Provider: modelStr, Model: ""}, nil
		}
	}
	// Also check prefix provider nodes for bare alias (e.g., custom prefixes)
	if info := h.resolvePrefixProvider(modelStr, ""); info != nil {
		return info, nil
	}

	// 4. Check common providers as a fallback
	for _, provider := range []string{"openai", "anthropic", "deepseek"} {
		conns, err := h.Repo.GetProviderConnections(provider, true)
		if err == nil && len(conns) > 0 {
			return &ModelInfo{Provider: provider, Model: modelStr}, nil
		}
	}

	return nil, fmt.Errorf("could not resolve model: %s", modelStr)
}

// resolvePrefixProvider checks if a provider name is a providerNode prefix.
// If so, it finds the matching connection and returns a pinned ModelInfo.
func (h *ChatHandler) resolvePrefixProvider(prefix string, model string) *ModelInfo {
	node, _, err := h.Repo.GetProviderNodeByPrefix(prefix)
	if err != nil || node == nil {
		return nil
	}

	conn, _, err := h.getBestConnection(node.ID, "", nil, model)
	if err != nil || conn == nil {
		return nil
	}

	return &ModelInfo{
		Provider:     node.ID,
		Model:        model,
		ConnectionID: conn.ID,
	}
}
