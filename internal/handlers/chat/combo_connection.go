package chat

import (
	"9router/proxy/internal/providers"
	"fmt"
)

// comboConnection resolves a concrete configured account first. Providers with
// a built-in public/default credential only fall back to the synthetic default
// route when no active local connection exists at all. This preserves account
// trust/cooldown/proxy/baseURL semantics for free-best and other combos.
func (h *ChatHandler) comboConnection(modelInfo *ModelInfo, excludeIDs []string) (string, *ConnectionData, bool, error) {
	conn, connData, err := h.getBestConnection(modelInfo.Provider, modelInfo.ConnectionID, excludeIDs, modelInfo.Model)
	if err == nil && conn != nil {
		// The virtual noauth connection is a single synthetic route; do not spin
		// through the same route repeatedly inside one combo entry.
		return conn.ID, connData, conn.ID == "noauth", nil
	}

	if modelInfo.ConnectionID != "" {
		return "", nil, false, err
	}

	cfg, ok := providers.KnownProviders[modelInfo.Provider]
	if !ok || (!cfg.NoAuth && cfg.DefaultAPIKey == "") {
		return "", nil, false, err
	}

	// Configured connections exist but none is selectable (locked/excluded/etc).
	// Do not bypass their account policy with the built-in default credential.
	connections, queryErr := h.Repo.GetProviderConnections(modelInfo.Provider, true)
	if queryErr != nil {
		if err != nil {
			return "", nil, false, err
		}
		return "", nil, false, queryErr
	}
	if len(connections) > 0 {
		if err != nil {
			return "", nil, false, err
		}
		return "", nil, false, fmt.Errorf("no selectable configured connection for provider %s", modelInfo.Provider)
	}

	apiKey := cfg.DefaultAPIKey
	if apiKey == "" {
		apiKey = "public"
	}
	return "default", &ConnectionData{APIKey: apiKey, ProxyPoolID: h.ResolveProviderProxyPoolID(modelInfo.Provider)}, true, nil
}
