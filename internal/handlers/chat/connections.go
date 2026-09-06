package chat

import (
	json "encoding/json/v2"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"9router/proxy/internal/constants"
	"9router/proxy/internal/log"
	"9router/proxy/internal/models"
	"9router/proxy/internal/providers"
	internalproxy "9router/proxy/internal/proxy"
)

// CredentialFallbacks maps search/tool providers to the primary chat provider whose API key can be reused.
var CredentialFallbacks = map[string]string{
	"ollama-search": "ollama",
	"zai-search":    "glm",
}

// GetBestConnection retrieves the highest-priority active connection for a provider.
// When connectionID is non-empty, it fetches that specific connection directly.
func (h *ChatHandler) GetBestConnection(provider string, connectionID string, excludeIDs []string, model string) (*models.ProviderConnection, *ConnectionData, error) {
	return h.getBestConnection(provider, connectionID, excludeIDs, model)
}

func (h *ChatHandler) getBestConnection(provider string, connectionID string, excludeIDs []string, model string) (*models.ProviderConnection, *ConnectionData, error) {
	if model != "" && !h.Repo.IsProviderAvailable(provider, model) {
		log.Warn("health", "unhealthy provider", "provider", provider, "model", model)
	}

	var conn *models.ProviderConnection
	var err error

	if connectionID != "" {
		conn, err = h.Repo.GetProviderConnectionByID(connectionID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch connection %s: %w", connectionID, err)
		}
		if conn == nil {
			return nil, nil, fmt.Errorf("connection %s not found", connectionID)
		}
	} else {
		connections, queryErr := h.Repo.GetProviderConnections(provider, true)
		if queryErr != nil {
			return nil, nil, fmt.Errorf("failed to query connections for %s: %w", provider, queryErr)
		}
		if len(connections) == 0 {
			if fallbackProvider, ok := CredentialFallbacks[provider]; ok {
				fallbackConns, fallbackErr := h.Repo.GetProviderConnections(fallbackProvider, true)
				if fallbackErr == nil && len(fallbackConns) > 0 {
					connections = fallbackConns
				}
			}
		}
		if len(connections) == 0 {
			if cfg, ok := providers.KnownProviders[provider]; ok && cfg.NoAuth {
				// Inject virtual connection for no-auth provider with optional proxy pool strategy from settings
				connData := &ConnectionData{
					AccessToken: "public",
				}
				settings, err := h.Repo.GetSettings()
				if err == nil && settings != nil && settings.ProviderStrategies != nil {
					if strat, ok := settings.ProviderStrategies[provider]; ok {
						if strat.ProxyPoolID != "" && strat.ProxyPoolID != "__none__" {
							connData.ProxyPoolID = strat.ProxyPoolID
						}
					}
				}
				publicName := "Public"
				conn := &models.ProviderConnection{
					ID:       "noauth",
					Provider: provider,
					Name:     &publicName,
					IsActive: 1,
				}
				return conn, connData, nil
			}
			return nil, nil, fmt.Errorf("no active connections for provider: %s", provider)
		}

		excludeSet := make(map[string]bool, len(excludeIDs))
		for _, id := range excludeIDs {
			excludeSet[id] = true
		}

		connections = h.rotateConnections(provider, connections)
		conn = nil
		for _, c := range connections {
			if excludeSet[c.ID] {
				continue
			}
			// Skip connections that have an active per-connection model lock
			if model != "" {
				if locked, _ := h.Repo.IsConnectionModelLocked(c.ID, model); locked {
					continue
				}
				if provider == "antigravity" && IsAntigravityModelBlocked(c.ID, model) {
					continue
				}
			}
			conn = c
			break
		}
		if conn == nil {
			return nil, nil, fmt.Errorf("no available connections for provider: %s (all excluded)", provider)
		}
	}

	// A pinned account must obey the same eligibility checks as automatic selection.
	if conn.IsActive != 1 || slices.Contains(excludeIDs, conn.ID) {
		return nil, nil, fmt.Errorf("connection unavailable")
	}
	if conn.Provider != provider && CredentialFallbacks[provider] != conn.Provider {
		return nil, nil, fmt.Errorf("connection provider mismatch")
	}
	if model != "" {
		locked, lockErr := h.Repo.IsConnectionModelLocked(conn.ID, model)
		if lockErr != nil || locked || (provider == "antigravity" && IsAntigravityModelBlocked(conn.ID, model)) {
			return nil, nil, fmt.Errorf("connection model unavailable")
		}
	}
	var connData ConnectionData
	if conn.Data != "" {
		if err := json.Unmarshal([]byte(conn.Data), &connData); err != nil {
			return nil, nil, fmt.Errorf("failed to parse connection data: %w", err)
		}
	}

	return conn, &connData, nil
}

// GetProviderConfig returns the upstream configuration for a provider.
func (h *ChatHandler) GetProviderConfig(provider string, connData *ConnectionData) (*providers.ProviderConfig, error) {
	return h.getProviderConfig(provider, connData)
}

func (h *ChatHandler) getProviderConfig(provider string, connData *ConnectionData) (*providers.ProviderConfig, error) {
	var baseCfg *providers.ProviderConfig

	if connData != nil && connData.BaseURL != "" {
		baseCfg = &providers.ProviderConfig{
			BaseURL:    connData.BaseURL,
			AuthHeader: constants.HeaderAuthorization,
			AuthScheme: constants.AuthSchemeBearer,
		}
	} else if cfg, ok := providers.KnownProviders[provider]; ok {
		// Clone config so per-request headers don't mutate global registry
		cloned := cfg
		baseCfg = &cloned
	} else {
		node, nodeData, err := h.Repo.GetProviderNodeByID(provider)
		if err != nil {
			return nil, fmt.Errorf("failed to look up provider node %s: %w", provider, err)
		}
		if node != nil && nodeData != nil && nodeData.BaseURL != "" {
			baseURL := nodeData.BaseURL
			if !strings.HasSuffix(baseURL, "/chat/completions") {
				if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v1/") {
					baseURL = strings.TrimRight(baseURL, "/") + "/chat/completions"
				} else {
					baseURL = strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
				}
			}
			baseCfg = &providers.ProviderConfig{
				BaseURL:    baseURL,
				AuthHeader: constants.HeaderAuthorization,
				AuthScheme: constants.AuthSchemeBearer,
			}
		}
	}

	if baseCfg == nil {
		return nil, fmt.Errorf("provider %q has no baseUrl in connection data and is not in KnownProviders", provider)
	}

	// Check if this connection uses an Edge Relay Proxy Pool (Vercel, Cloudflare, Deno)
	if connData != nil {
		var relayURL string
		var noProxy string

		if connData.ProxyPoolID != "" {
			if pool, err := h.Repo.GetProxyPool(connData.ProxyPoolID); err == nil && pool != nil && pool.IsActive {
				if pool.Type == "vercel" || pool.Type == "cloudflare" || pool.Type == "deno" {
					relayURL = pool.NextURL()
					noProxy = pool.NoProxy
				}
			}
		}
		if relayURL == "" && connData.ProviderSpecificData != nil {
			if u, ok := connData.ProviderSpecificData["vercelRelayUrl"].(string); ok && u != "" {
				relayURL = u
				if np, ok := connData.ProviderSpecificData["connectionNoProxy"].(string); ok {
					noProxy = np
				}
			}
		}

		if relayURL != "" && !internalproxy.ShouldBypassNoProxy(baseCfg.BaseURL, noProxy) {
			cloned := *baseCfg
			cloned.StaticHeaders = internalproxy.BuildEdgeRelayHeaders(baseCfg.BaseURL, cloned.StaticHeaders)
			cloned.BaseURL = relayURL
			return &cloned, nil
		}
	}

	return baseCfg, nil
}

// ExtractAPIKey gets the API key from a connection's data.
func ExtractAPIKey(connData *ConnectionData) string {
	return extractAPIKey(connData)
}

func extractAPIKey(connData *ConnectionData) string {
	if connData.APIKey != "" {
		return connData.APIKey
	}
	return connData.AccessToken
}

// GetClientForConnection returns an http.Client configured with ProxyPool transport if set.
func (h *ChatHandler) GetClientForConnection(connData *ConnectionData) *http.Client {
	return h.getClientForConnection(connData)
}

func (h *ChatHandler) getClientForConnection(connData *ConnectionData) *http.Client {
	if connData == nil {
		return h.Client
	}

	var proxyURLStr string
	var proxyType string
	var strictProxy bool

	// 1. Resolve from ProxyPool
	if connData.ProxyPoolID != "" {
		pool, err := h.Repo.GetProxyPool(connData.ProxyPoolID)
		if err == nil && pool != nil && pool.IsActive {
			proxyURLStr = pool.NextURL()
			proxyType = pool.Type
			strictProxy = pool.StrictProxy
		}
	}

	// 2. Fallback to legacy connection proxy
	if proxyURLStr == "" {
		proxyEnabled := connData.ConnectionProxyEnabled
		proxyURL := connData.ConnectionProxyURL
		if !proxyEnabled && connData.ProviderSpecificData != nil {
			if en, ok := connData.ProviderSpecificData["connectionProxyEnabled"].(bool); ok {
				proxyEnabled = en
			}
			if u, ok := connData.ProviderSpecificData["connectionProxyUrl"].(string); ok {
				proxyURL = u
			}
			if sp, ok := connData.ProviderSpecificData["strictProxy"].(bool); ok {
				strictProxy = sp
			}
		}
		if proxyEnabled && proxyURL != "" {
			proxyURLStr = proxyURL
			proxyType = "http"
		}
	}

	if proxyURLStr == "" {
		return h.Client
	}

	parsedURL, err := url.Parse(proxyURLStr)
	if err != nil {
		log.Warn("proxy", "invalid proxy pool url", "pool", connData.ProxyPoolID, "url", proxyURLStr, "error", err)
		if strictProxy {
			log.Error("proxy", "strict proxy enabled but proxy url invalid", "url", proxyURLStr)
		}
		return h.Client
	}

	if proxyType == "http" || proxyType == "" {
		transport := &http.Transport{
			Proxy: http.ProxyURL(parsedURL),
		}
		return &http.Client{
			Transport: transport,
			Timeout:   h.Client.Timeout,
		}
	}

	// For Edge Relays (vercel, cloudflare, deno), standard client is used because
	// URL rewriting and x-relay headers are handled at request time.
	return h.Client
}

// Rotate only within a priority tier; lower-priority accounts remain fallbacks.
func (h *ChatHandler) rotateConnections(provider string, conns []*models.ProviderConnection) []*models.ProviderConnection {
	settings, err := h.Repo.GetSettings()
	if err != nil || settings == nil {
		return conns
	}
	strategy := settings.ProviderStrategies[provider].RotateStrategy
	if strategy != "round-robin" && strategy != "random" {
		return conns
	}
	out := append([]*models.ProviderConnection(nil), conns...)
	h.accountMu.Lock()
	defer h.accountMu.Unlock()
	if h.accountTurns == nil {
		h.accountTurns = make(map[string]uint64)
	}
	turn := h.accountTurns[provider]
	h.accountTurns[provider]++
	priority := func(c *models.ProviderConnection) int {
		if c.Priority == nil {
			return 999999
		}
		return *c.Priority
	}
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && priority(out[j]) == priority(out[i]) {
			j++
		}
		n := j - i
		offset := int(turn % uint64(n))
		if strategy == "random" {
			offset = rand.IntN(n)
		}
		group := append([]*models.ProviderConnection(nil), out[i:j]...)
		sort.Slice(group, func(a, b int) bool { return group[a].ID < group[b].ID })
		for k := 0; k < n; k++ {
			out[i+k] = group[(k+offset)%n]
		}
		i = j
	}
	return out
}
