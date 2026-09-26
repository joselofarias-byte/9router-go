package chat

import (
	"regexp"
	"strings"

	json "encoding/json/v2"

	"9router/proxy/internal/db"
	"9router/proxy/internal/models"
	"9router/proxy/internal/providers"
)

// ModelInfoObject represents a model entry in the /v1/models response. The
// key set mirrors upstream exactly: id, object, owned_by, capabilities and
// (for LLM entries) context_length / max_completion_tokens.
type ModelInfoObject struct {
	ID                  string                        `json:"id"`
	Object              string                        `json:"object"`
	OwnedBy             string                        `json:"owned_by"`
	Capabilities        *providers.CapabilitiesDetail `json:"capabilities,omitempty"`
	ContextLength       int                           `json:"context_length,omitempty"`
	MaxCompletionTokens int                           `json:"max_completion_tokens,omitempty"`
}

// ConnectionHasCredential reports whether a provider connection carries auth
// material that can actually serve requests (apiKey, accessToken, authToken, or
// refreshToken; refreshToken alone is accepted because the token pipeline
// refreshes it on demand). Exported for dashboard consistency checks.
//
// NOTE: /v1/models listing itself does NOT use this — upstream
// src/app/api/v1/models/route.js filters connections on isActive only.
func ConnectionHasCredential(conn *models.ProviderConnection) bool {
	if conn == nil || conn.Data == "" {
		return false
	}
	var data struct {
		APIKey       string `json:"apiKey"`
		AccessToken  string `json:"accessToken"`
		AuthToken    string `json:"authToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.Unmarshal([]byte(conn.Data), &data); err != nil {
		return false
	}
	return data.APIKey != "" || data.AccessToken != "" || data.AuthToken != "" || data.RefreshToken != ""
}

// connectionHasCredential is the package-local alias.
func connectionHasCredential(conn *models.ProviderConnection) bool {
	return ConnectionHasCredential(conn)
}

// Upstream kind hints for model ids with no per-model type metadata
// (src/app/api/v1/models/route.js inferKindFromUnknownModelId). The default
// /v1/models build uses kindFilter ["llm"], so anything these match is a
// media/embedding model and stays out of the LLM list.
var (
	embeddingIDHint = regexp.MustCompile(`(?i)embed`)
	ttsIDHint       = regexp.MustCompile(`(?i)tts|speech|audio|voice`)
	imageIDHint     = regexp.MustCompile(`(?i)image|imagen|dall-?e|flux|sdxl|sd-|stable-diffusion`)
)

// isLLMModelID mirrors upstream inferKindFromUnknownModelId.
func isLLMModelID(modelID string) bool {
	switch {
	case embeddingIDHint.MatchString(modelID):
		return false
	case ttsIDHint.MatchString(modelID):
		return false
	case imageIDHint.MatchString(modelID):
		return false
	default:
		return true
	}
}

// isLLMCustomModel mirrors upstream modelKind() for a kv.customModels row: the
// row's own `type` decides, and an unknown/absent type is an LLM.
func isLLMCustomModel(modelType string) bool {
	switch strings.ToLower(strings.TrimSpace(modelType)) {
	case "", "llm", "chat":
		return true
	default:
		return false
	}
}

// connectionModelData is the subset of the connection data blob upstream reads
// (providerSpecificData.prefix / providerSpecificData.enabledModels, plus the
// top-level variants older dashboards wrote).
type connectionModelData struct {
	Prefix               string   `json:"prefix"`
	EnabledModels        []string `json:"enabledModels"`
	ProviderSpecificData struct {
		Prefix        string   `json:"prefix"`
		EnabledModels []string `json:"enabledModels"`
	} `json:"providerSpecificData"`
}

func parseConnectionModelData(conn *models.ProviderConnection) connectionModelData {
	var data connectionModelData
	if conn != nil && conn.Data != "" {
		_ = json.Unmarshal([]byte(conn.Data), &data)
	}
	return data
}

// stripModelPrefix removes a leading "<prefix>/" qualifier the way upstream
// strips outputAlias/, staticAlias/ and providerId/ from rawModelIds.
func stripModelPrefix(modelID string, prefixes ...string) string {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if strings.HasPrefix(modelID, p+"/") {
			return modelID[len(p)+1:]
		}
	}
	return modelID
}

func isCompatibleProviderID(providerID string) bool {
	return strings.HasPrefix(providerID, "openai-compatible-") ||
		strings.HasPrefix(providerID, "anthropic-compatible-")
}

// disabledModelIndex collects the `disabledModels` KV scope the dashboard
// writes to, keyed by both the stored alias and its canonical provider.
func (h *ChatHandler) disabledModelIndex() map[string]map[string]bool {
	disabled := make(map[string]map[string]bool)
	if h.Repo == nil {
		return disabled
	}
	disabledKV, err := h.Repo.GetKVScope("disabledModels")
	if err != nil {
		return disabled
	}
	for prov, raw := range disabledKV {
		var ids []string
		if perr := json.Unmarshal([]byte(raw), &ids); perr != nil {
			continue
		}
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				set[id] = true
			}
		}
		if len(set) == 0 {
			continue
		}
		disabled[prov] = set
		if canon := providers.ResolveAlias(prov); canon != "" {
			if _, ok := disabled[canon]; !ok {
				disabled[canon] = set
			}
		}
	}
	return disabled
}

// buildModelsList mirrors upstream buildModelsList(["llm"]):
// combos first, then one model set per active connection, with the static
// catalog dump only when the connections table itself is empty.
func (h *ChatHandler) buildModelsList() []ModelInfoObject {
	var data []ModelInfoObject
	seen := make(map[string]bool)
	disabled := h.disabledModelIndex()
	isDisabled := func(provider, modelID string) bool {
		return disabled[provider][modelID]
	}

	// 1. Combos first (upstream pushes them before provider models).
	data = h.appendCombos(data, seen)

	var allConns []*models.ProviderConnection
	if h.Repo != nil {
		allConns, _ = h.Repo.GetProviderConnections("", false)
	}

	// 2. No connection rows at all: static catalog dump + custom models, so a
	// fresh install still has a usable picker (upstream connections.length === 0).
	if len(allConns) == 0 {
		for alias, models := range providers.ProviderModels {
			if canon := providers.ResolveAlias(alias); canon != alias && providers.GetProviderAlias(canon) != alias {
				continue
			}
			for _, mID := range models {
				if !isLLMModelID(mID) || isDisabled(alias, mID) {
					continue
				}
				data = appendStaticModel(data, seen, alias, mID)
			}
		}
		data = h.appendLooseCustomModels(data, seen, disabled)
		return finalizeModels(data)
	}

	// 3. One connection per provider, isActive !== false (upstream filters on
	// isActive only — no credential requirement for listing).
	firstPerProvider := make(map[string]*models.ProviderConnection)
	order := make([]string, 0, len(allConns))
	for _, conn := range allConns {
		if conn.Provider == "" || conn.IsActive == 0 {
			continue
		}
		if _, ok := firstPerProvider[conn.Provider]; !ok {
			firstPerProvider[conn.Provider] = conn
			order = append(order, conn.Provider)
		}
	}

	customs := h.customModelsByProvider()
	aliases := h.modelAliasTargets()

	for _, provID := range order {
		conn := firstPerProvider[provID]
		data = h.appendConnectionModels(data, seen, conn, provID, customs, aliases, isDisabled)
	}

	return finalizeModels(data)
}

// appendConnectionModels reproduces the per-connection branch of upstream
// buildModelsList: enabledModels override, else the static catalog, merged with
// custom models and alias targets registered for that same provider.
func (h *ChatHandler) appendConnectionModels(
	data []ModelInfoObject,
	seen map[string]bool,
	conn *models.ProviderConnection,
	providerID string,
	customs map[string][]*db.CustomModel,
	aliases map[string]string,
	isDisabled func(provider, modelID string) bool,
) []ModelInfoObject {
	connData := parseConnectionModelData(conn)

	staticAlias := providers.GetProviderAlias(providerID)
	if staticAlias == "" {
		staticAlias = providerID
	}
	outputAlias := staticAlias
	switch {
	case connData.ProviderSpecificData.Prefix != "":
		outputAlias = connData.ProviderSpecificData.Prefix
	case connData.Prefix != "":
		outputAlias = connData.Prefix
	}

	ids := connData.ProviderSpecificData.EnabledModels
	if len(ids) == 0 {
		ids = connData.EnabledModels
	}
	if len(ids) == 0 && !isCompatibleProviderID(providerID) {
		ids = providers.GetProviderModels(staticAlias)
		if len(ids) == 0 {
			ids = providers.GetProviderModels(outputAlias)
		}
	}

	merged := make([]string, 0, len(ids))
	seenID := make(map[string]bool, len(ids))
	typedCustom := make(map[string]bool, len(ids))
	add := func(raw string, fromCustom bool) {
		modelID := strings.TrimSpace(stripModelPrefix(raw, outputAlias, staticAlias, providerID))
		if modelID == "" || seenID[modelID] {
			return
		}
		seenID[modelID] = true
		typedCustom[modelID] = fromCustom
		merged = append(merged, modelID)
	}
	for _, raw := range ids {
		add(raw, false)
	}
	// Custom models registered for this provider. Upstream matches
	// alias === staticAlias || outputAlias || providerId.
	for _, key := range []string{staticAlias, outputAlias, providerID} {
		for _, cm := range customs[key] {
			if !isLLMCustomModel(cm.Type) {
				continue
			}
			// Upstream resolves custom-model capabilities from the saved caps,
			// so publish them before the capability lookup below.
			h.registerCustomModelCaps(outputAlias, key, cm)
			add(cm.ID, true)
		}
	}
	// Legacy alias targets pointing at this provider.
	for _, target := range aliases {
		if strings.HasPrefix(target, outputAlias+"/") ||
			strings.HasPrefix(target, staticAlias+"/") ||
			strings.HasPrefix(target, providerID+"/") {
			add(target, false)
		}
	}

	for _, modelID := range merged {
		if isDisabled(outputAlias, modelID) || isDisabled(staticAlias, modelID) {
			continue
		}
		// Upstream resolves kind from the row's own type for custom models, so
		// the id heuristic only applies to ids without type metadata.
		if !typedCustom[modelID] && !isLLMModelID(modelID) {
			continue
		}
		fullID := outputAlias + "/" + modelID
		if seen[fullID] {
			continue
		}
		seen[fullID] = true

		ctxLen, maxOut := providers.GetModelTokenLimits(modelID)
		if ctxLen == 0 && maxOut == 0 {
			ctxLen, maxOut = providers.GetModelTokenLimits(fullID)
		}
		caps := providers.GetCapabilitiesDetailForModel(providerID, modelID)
		if caps.ContextWindow > 0 && ctxLen == 0 {
			ctxLen = caps.ContextWindow
		}
		if maxOut == 0 {
			maxOut = caps.MaxOutput
		}
		data = append(data, ModelInfoObject{
			ID:                  fullID,
			Object:              "model",
			OwnedBy:             outputAlias,
			Capabilities:        &caps,
			ContextLength:       ctxLen,
			MaxCompletionTokens: maxOut,
		})
	}
	return data
}

// appendStaticModel emits one static-registry entry (fresh-install path).
func appendStaticModel(data []ModelInfoObject, seen map[string]bool, alias, modelID string) []ModelInfoObject {
	fullID := alias + "/" + modelID
	if seen[fullID] {
		return data
	}
	seen[fullID] = true

	ctxLen, maxOut := providers.GetModelTokenLimits(modelID)
	caps := providers.GetCapabilitiesDetailForModel(alias, modelID)
	if caps.ContextWindow > 0 && ctxLen == 0 {
		ctxLen = caps.ContextWindow
	}
	if maxOut == 0 {
		maxOut = caps.MaxOutput
	}
	return append(data, ModelInfoObject{
		ID:                  fullID,
		Object:              "model",
		OwnedBy:             alias,
		Capabilities:        &caps,
		ContextLength:       ctxLen,
		MaxCompletionTokens: maxOut,
	})
}

// appendLooseCustomModels lists custom models on a fresh install (upstream
// lists every llm-typed custom row when there are no connections at all).
func (h *ChatHandler) appendLooseCustomModels(data []ModelInfoObject, seen map[string]bool, disabled map[string]map[string]bool) []ModelInfoObject {
	prefixMap := h.providerNodePrefixMap()
	customs := h.customModelsByProvider()
	for providerID, list := range customs {
		prefix := providerID
		if mapped, ok := prefixMap[providerID]; ok && mapped != "" {
			prefix = mapped
		}
		for _, cm := range list {
			if !isLLMCustomModel(cm.Type) {
				continue
			}
			if disabled[providerID][cm.ID] || disabled[prefix][cm.ID] {
				continue
			}
			h.registerCustomModelCaps(prefix, providerID, cm)
			if !isLLMModelID(cm.ID) {
				continue
			}
			fullID := prefix + "/" + cm.ID
			if seen[fullID] {
				continue
			}
			seen[fullID] = true
			ctxLen, maxOut := providers.GetModelTokenLimits(fullID)
			if ctxLen == 0 && maxOut == 0 {
				ctxLen, maxOut = providers.GetModelTokenLimits(cm.ID)
			}
			caps := providers.GetCapabilitiesDetailForModel(prefix, cm.ID)
			if caps.ContextWindow > 0 && ctxLen == 0 {
				ctxLen = caps.ContextWindow
			}
			if maxOut == 0 {
				maxOut = caps.MaxOutput
			}
			data = append(data, ModelInfoObject{
				ID:                  fullID,
				Object:              "model",
				OwnedBy:             prefix,
				Capabilities:        &caps,
				ContextLength:       ctxLen,
				MaxCompletionTokens: maxOut,
			})
		}
	}
	return data
}

// registerCustomModelCaps publishes the capability flags saved on a custom model
// so capability lookups elsewhere see them, like upstream customModelCaps.
func (h *ChatHandler) registerCustomModelCaps(prefix, providerID string, cm *db.CustomModel) {
	if len(cm.Caps) == 0 {
		return
	}
	var caps providers.Capabilities
	if cm.Caps["vision"] {
		caps.Vision = true
	}
	if cm.Caps["reasoning"] {
		caps.Reasoning = true
	}
	if cm.Caps["search"] {
		caps.Search = true
	}
	if cm.Caps["tools"] {
		caps.Tools = true
	}
	if cm.Caps["image"] || cm.Caps["imageOutput"] {
		caps.ImageOutput = true
	}
	if cm.Caps["audio"] {
		caps.AudioInput = true
	}
	providers.SetCustomModelCaps(prefix, cm.ID, caps)
	if prefix != providerID {
		providers.SetCustomModelCaps(providerID, cm.ID, caps)
	}
}

// appendCombos lists combo names with the union of their leaf capabilities,
// matching upstream aggregateComboCapabilities.
func (h *ChatHandler) appendCombos(data []ModelInfoObject, seen map[string]bool) []ModelInfoObject {
	if h.Repo == nil {
		return data
	}
	combos, err := h.Repo.GetCombos()
	if err != nil {
		return data
	}
	for _, combo := range combos {
		if combo == nil || combo.Name == "" || seen[combo.Name] {
			continue
		}
		// Upstream comboMatchesKinds: this endpoint is the LLM list
		// (kindFilter ["llm"]), so webSearch/webFetch combos are excluded —
		// they only appear under /v1/models/web. No kind (or an explicit
		// "llm" kind) means an LLM combo.
		if combo.Kind != nil {
			if kind := strings.TrimSpace(*combo.Kind); kind != "" && kind != "llm" {
				continue
			}
		}
		seen[combo.Name] = true

		entry := ModelInfoObject{
			ID:      combo.Name,
			Object:  "model",
			OwnedBy: "combo",
		}
		// Upstream combo entries carry capabilities only — no
		// context_length / max_completion_tokens.
		if caps, ok := h.aggregateComboCapabilities(combo.Name); ok {
			entry.Capabilities = caps
		}
		data = append(data, entry)
	}
	return data
}

// aggregateComboCapabilities merges the capabilities of every leaf model so a
// combo advertises the union of what its members support (upstream).
func (h *ChatHandler) aggregateComboCapabilities(comboName string) (*providers.CapabilitiesDetail, bool) {
	combo, err := h.Repo.GetComboByName(comboName)
	if err != nil || combo == nil || combo.Models == "" {
		return nil, false
	}
	var leaves []string
	if err := json.Unmarshal([]byte(combo.Models), &leaves); err != nil || len(leaves) == 0 {
		return nil, false
	}
	flattened, flatErr := h.flattenComboModels(leaves)
	if flatErr != nil {
		flattened = leaves
	}

	merged := providers.CapabilitiesDetail{}
	found := false
	for _, leaf := range flattened {
		info := h.resolveModelEntry(leaf)
		providerID, modelID := "", leaf
		if info != nil {
			providerID, modelID = info.Provider, info.Model
		}
		caps := providers.GetCapabilitiesDetailForModel(providerID, modelID)
		merged = providers.MergeCapabilitiesDetail(merged, caps)
		found = true
	}
	if !found {
		return nil, false
	}
	return &merged, true
}

func finalizeModels(data []ModelInfoObject) []ModelInfoObject {
	if data == nil {
		return []ModelInfoObject{}
	}
	return data
}

// providerNodePrefixMap maps providerNode.id → its registered display prefix.
func (h *ChatHandler) providerNodePrefixMap() map[string]string {
	if h.Repo == nil {
		return nil
	}
	prefixMap, err := h.Repo.GetProviderNodePrefixMap()
	if err != nil {
		return nil
	}
	return prefixMap
}

// customModelsByProvider indexes kv.customModels rows by their stored
// providerAlias so a connection can pick up the models registered for it.
func (h *ChatHandler) customModelsByProvider() map[string][]*db.CustomModel {
	out := make(map[string][]*db.CustomModel)
	if h.Repo == nil {
		return out
	}
	customs, err := h.Repo.GetCustomModels()
	if err != nil {
		return out
	}
	for _, cm := range customs {
		if cm == nil || cm.ProviderAlias == "" || cm.ID == "" {
			continue
		}
		out[cm.ProviderAlias] = append(out[cm.ProviderAlias], cm)
	}
	return out
}

// modelAliasTargets returns the alias name → target model mapping. Upstream
// merges those targets into the owning provider's list instead of publishing
// the alias key as its own model.
func (h *ChatHandler) modelAliasTargets() map[string]string {
	out := make(map[string]string)
	if h.Repo == nil {
		return out
	}
	aliases, err := h.Repo.GetModelAliases()
	if err != nil {
		return out
	}
	for name, target := range aliases {
		if name == "" || target == "" {
			continue
		}
		out[name] = target
	}
	return out
}
