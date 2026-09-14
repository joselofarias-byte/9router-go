package chat

import (
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlers/shared"
	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy/executor"
	"9router/proxy/internal/proxy/oauth"
)

// NewChatHandler creates a ChatHandler with the given repository and a streaming-capable HTTP client.
func NewChatHandler(repo *db.Repo, ts ...*shared.TokenSaverConfig) *ChatHandler {
	executor.RegisterAll()
	oauth.RegisterAll()
	cfg := &shared.TokenSaverConfig{}
	if len(ts) > 0 && ts[0] != nil {
		cfg = ts[0]
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	return &ChatHandler{Repo: repo, Client: &http.Client{Transport: transport, Timeout: 0}, TokenSaver: cfg, stickyState: make(map[string]*comboStickyState)}
}

func (h *ChatHandler) ResolveModel(modelStr string) (*ModelInfo, error) { return h.resolveModel(modelStr) }

func resolveProviderAlias(alias string) string {
	if canonical, ok := providers.ProviderAliasMap[alias]; ok { return canonical }
	return alias
}

func (h *ChatHandler) resolveModelEntry(entry string) *ModelInfo {
	if !strings.Contains(entry, "/") {
		combo, err := h.Repo.GetComboByName(entry)
		if err == nil && combo != nil && combo.Models != "" {
			var subModels []string
			if err := json.Unmarshal([]byte(combo.Models), &subModels); err == nil && len(subModels) > 0 {
				first := h.resolveModelEntry(subModels[0])
				if first != nil { first.ComboModels = subModels; first.Strategy = combo.Strategy; return first }
			}
		}
		return nil
	}
	parts := strings.SplitN(entry, "/", 2)
	provider := resolveProviderAlias(parts[0])
	if _, ok := providers.KnownProviders[provider]; !ok {
		if info := h.resolvePrefixProvider(provider, parts[1]); info != nil { return info }
	}
	return &ModelInfo{Provider: provider, Model: parts[1]}
}

func (h *ChatHandler) flattenComboModels(models []string) ([]string, error) {
	out := make([]string, 0, len(models)); seen := make(map[string]bool)
	var walk func([]string) error
	walk = func(ms []string) error {
		for _, m := range ms {
			if !strings.Contains(m, "/") {
				if seen[m] { log.Warn("combo", "cyclic combo reference detected, skipping", "combo", m); continue }
				if combo, err := h.Repo.GetComboByName(m); err == nil && combo != nil && combo.Models != "" {
					var sub []string
					if err := json.Unmarshal([]byte(combo.Models), &sub); err == nil { seen[m] = true; if err := walk(sub); err != nil { return err }; delete(seen, m); continue }
				}
				if aliasTarget, err := h.Repo.GetModelAlias(m); err == nil && aliasTarget != "" && strings.Contains(aliasTarget, "/") { m = aliasTarget }
			}
			if len(out) == 0 || out[len(out)-1] != m { out = append(out, m) }
		}
		return nil
	}
	if err := walk(models); err != nil { return nil, err }
	if len(out) == 0 { return nil, fmt.Errorf("combo has no valid leaf models") }
	return out, nil
}

func stripModelContextMarker(modelStr string) string {
	trimmed := strings.TrimSpace(modelStr)
	if len(trimmed) < 4 { return modelStr }
	suffix := trimmed[len(trimmed)-4:]
	if strings.EqualFold(suffix, "[1m]") { return strings.TrimSpace(trimmed[:len(trimmed)-4]) }
	return modelStr
}

// resolveDynamicFreeBest builds a transient fallback combo from currently discovered
// free/free-tier routes that also have an active local provider connection.
func (h *ChatHandler) resolveDynamicFreeBest() (*ModelInfo, error) {
	candidates := getPolicyCandidates(nil, h.Repo.RawDB(), "", routing.PolicyFreeOnly)
	if len(candidates) == 0 { return nil, fmt.Errorf("free-best: no discovered free models with active connections") }

	models := make([]string, 0, len(candidates)); seen := make(map[string]bool)
	for _, c := range candidates {
		model := c.UpstreamModel
		if model == "" { model = c.ModelID }
		entry := c.ProviderID + "/" + model
		if seen[entry] { continue }
		seen[entry] = true
		models = append(models, entry)
	}
	if len(models) == 0 { return nil, fmt.Errorf("free-best: no routable free models") }
	first := h.resolveModelEntry(models[0])
	if first == nil { return nil, fmt.Errorf("free-best: failed to resolve first candidate") }
	first.ComboModels = models
	first.Strategy = "fallback"
	return first, nil
}

func (h *ChatHandler) resolveModel(modelStr string) (*ModelInfo, error) {
	if modelStr == "" { return nil, fmt.Errorf("missing model") }
	modelStr = stripModelContextMarker(modelStr)

	// Built-in virtual route: no DB combo setup required.
	if strings.EqualFold(modelStr, "free-best") || strings.EqualFold(modelStr, "free") {
		return h.resolveDynamicFreeBest()
	}

	if strings.Contains(modelStr, "/") {
		parts := strings.SplitN(modelStr, "/", 2); providerAlias := parts[0]; model := parts[1]; provider := resolveProviderAlias(providerAlias)
		if _, ok := providers.KnownProviders[provider]; !ok { if info := h.resolvePrefixProvider(provider, model); info != nil { return info, nil } }
		return &ModelInfo{Provider: provider, Model: model}, nil
	}

	aliasTarget, err := h.Repo.GetModelAlias(modelStr)
	if err == nil && aliasTarget != "" && strings.Contains(aliasTarget, "/") {
		parts := strings.SplitN(aliasTarget, "/", 2); provider := resolveProviderAlias(parts[0])
		if _, ok := providers.KnownProviders[provider]; !ok { if info := h.resolvePrefixProvider(provider, parts[1]); info != nil { return info, nil } }
		return &ModelInfo{Provider: provider, Model: parts[1]}, nil
	}

	combo, err := h.Repo.GetComboByName(modelStr)
	if err == nil && combo != nil && combo.Models != "" {
		var modelStrings []string
		if err := json.Unmarshal([]byte(combo.Models), &modelStrings); err == nil && len(modelStrings) > 0 {
			flattened, flatErr := h.flattenComboModels(modelStrings); if flatErr != nil { return nil, flatErr }
			if len(flattened) > 0 { firstInfo := h.resolveModelEntry(flattened[0]); if firstInfo == nil { firstInfo, _ = h.resolveModel(flattened[0]) }; if firstInfo != nil { firstInfo.ComboModels = flattened; firstInfo.Strategy = combo.Strategy; return firstInfo, nil } }
		}
	}

	if canonical := resolveProviderAlias(modelStr); canonical != modelStr {
		if _, ok := providers.KnownProviders[canonical]; ok { if conns, err := h.Repo.GetProviderConnections(canonical, true); err == nil && len(conns) > 0 { return &ModelInfo{Provider: canonical, Model: ""}, nil } }
	}
	if _, ok := providers.KnownProviders[modelStr]; ok { if conns, err := h.Repo.GetProviderConnections(modelStr, true); err == nil && len(conns) > 0 { return &ModelInfo{Provider: modelStr, Model: ""}, nil } }
	if info := h.resolvePrefixProvider(modelStr, ""); info != nil { return info, nil }

	for _, provider := range []string{"openai", "anthropic", "deepseek"} {
		conns, err := h.Repo.GetProviderConnections(provider, true); if err == nil && len(conns) > 0 { return &ModelInfo{Provider: provider, Model: modelStr}, nil }
	}
	return nil, fmt.Errorf("could not resolve model: %s", modelStr)
}

func (h *ChatHandler) resolvePrefixProvider(prefix string, model string) *ModelInfo {
	node, _, err := h.Repo.GetProviderNodeByPrefix(prefix); if err != nil || node == nil { return nil }
	conn, _, err := h.getBestConnection(node.ID, "", nil, model); if err != nil || conn == nil { return nil }
	return &ModelInfo{Provider: node.ID, Model: model, ConnectionID: conn.ID}
}
