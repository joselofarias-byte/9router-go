package chat

import (
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
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

	// Phase 5 Data Plane Integration: Intercept candidate lookup gracefully
	if connectionID == "" && model != "" {
		candidates := getActiveCandidates(nil, h.Repo.RawDB(), model)
		if len(candidates) > 0 {
			// Find the best valid candidate that matches requested provider (if specified) and is not excluded
			for _, cand := range candidates {
				if provider != "" && cand.ProviderID != provider {
					continue
				}
				excluded := false
				for _, ex := range excludeIDs {
					if ex == cand.AccountID {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}

				// Candidate is good. Fetch real DB credentials using the routed AccountID.
				cpConn, cpErr := h.Repo.GetProviderConnectionByID(cand.AccountID)
				if cpErr == nil && cpConn != nil && cpConn.IsActive == 1 {
					// Re-check dynamic blocks that the static CP policy might have missed
					if locked, _ := h.Repo.IsConnectionModelLocked(cpConn.ID, model); locked {
						continue
					}
					if cand.ProviderID == "antigravity" && IsAntigravityModelBlocked(cpConn.ID, model) {
						continue
					}

					log.Info("routing", "control plane route selected", "model", model, "provider", cand.ProviderID, "account", cand.AccountID)
					var data ConnectionData
					if err := json.Unmarshal([]byte(cpConn.Data), &data); err != nil {
						return nil, nil, fmt.Errorf("failed to parse connection data for account %s: %w", cand.AccountID, err)
					}
					return cpConn, &data, nil
				}
			}
		}
	}

	// Fallback to legacy behavior if Control Plane yields no candidates or is missing state
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
		if conn.IsActive != 1 {
			return nil, nil, fmt.Errorf("connection %s is inactive", connectionID)
		}
		if model != "" {
			locked, lockErr := h.Repo.IsConnectionModelLocked(conn.ID, model)
			if lockErr != nil {
				return nil, nil, fmt.Errorf("check connection %s model lock: %w", conn.ID, lockErr)
			}
			if locked {
				return nil, nil, fmt.Errorf("connection %s unavailable for model %s: cooldown active", conn.ID, model)
			}
			if conn.Provider == "antigravity" && IsAntigravityModelBlocked(conn.ID, model) {
				return nil, nil, fmt.Errorf("connection %s unavailable for model %s: provider model block active", conn.ID, model)
			}
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
		if cfg, ok := providers.KnownProviders[provider]; ok {
			cloned := cfg
			cloned.BaseURL = connData.BaseURL
			baseCfg = &cloned
		} else {
			baseCfg = &providers.ProviderConfig{
				BaseURL:    connData.BaseURL,
				AuthHeader: constants.HeaderAuthorization,
				AuthScheme: constants.AuthSchemeBearer,
			}
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

type failingRoundTripper struct {
	err error
}

func (t failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
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
	strictProxy := connData.StrictProxy
	if connData.ProviderSpecificData != nil {
		if sp, ok := connData.ProviderSpecificData["strictProxy"].(bool); ok && sp {
			strictProxy = true
		}
	}

	failClosed := func(reason string, args ...any) *http.Client {
		proxyErr := fmt.Errorf(reason, args...)
		log.Error("proxy", "strict proxy configuration unavailable; failing closed", "pool", connData.ProxyPoolID, "error", proxyErr)
		return &http.Client{
			Transport: failingRoundTripper{err: proxyErr},
			Timeout:   h.Client.Timeout,
		}
	}

	// 1. Resolve from ProxyPool. Read strictness even for inactive pools so an
	// explicit strict policy cannot silently turn into direct egress.
	if connData.ProxyPoolID != "" {
		pool, err := h.Repo.GetProxyPool(connData.ProxyPoolID)
		if err != nil || pool == nil {
			if strictProxy {
				return failClosed("strict proxy pool %q is unavailable", connData.ProxyPoolID)
			}
			log.Warn("proxy", "proxy pool unavailable; considering legacy/direct fallback", "pool", connData.ProxyPoolID, "error", err)
		} else {
			strictProxy = strictProxy || pool.StrictProxy
			if !pool.IsActive {
				if strictProxy {
					return failClosed("strict proxy pool %q is inactive", connData.ProxyPoolID)
				}
			} else {
				proxyURLStr = pool.NextURL()
				proxyType = pool.Type
				if proxyURLStr == "" && strictProxy {
					return failClosed("strict proxy pool %q has no usable URL", connData.ProxyPoolID)
				}
			}
		}
	}

	// 2. Fallback to legacy connection proxy
	if proxyURLStr == "" {
		proxyEnabled := connData.ConnectionProxyEnabled
		proxyURL := connData.ConnectionProxyURL
		if connData.ProviderSpecificData != nil {
			if !proxyEnabled {
				if en, ok := connData.ProviderSpecificData["connectionProxyEnabled"].(bool); ok {
					proxyEnabled = en
				}
			}
			if proxyURL == "" {
				if u, ok := connData.ProviderSpecificData["connectionProxyUrl"].(string); ok {
					proxyURL = u
				}
			}
		}
		if proxyEnabled && proxyURL != "" {
			proxyURLStr = proxyURL
			proxyType = "http"
		}
	}

	if proxyURLStr == "" {
		if strictProxy {
			return failClosed("strict proxy is enabled but no usable proxy route is configured")
		}
		return h.Client
	}

	parsedURL, err := url.Parse(proxyURLStr)
	if err != nil {
		log.Warn("proxy", "invalid proxy pool url", "pool", connData.ProxyPoolID, "url", proxyURLStr, "error", err)
		if strictProxy {
			proxyErr := fmt.Errorf("strict proxy configuration invalid for %q: %w", proxyURLStr, err)
			log.Error("proxy", "strict proxy enabled but proxy url invalid; failing closed", "url", proxyURLStr, "error", proxyErr)
			return &http.Client{
				Transport: failingRoundTripper{err: proxyErr},
				Timeout:   h.Client.Timeout,
			}
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

	// For supported Edge Relays, the standard client is intentional because URL
	// rewriting and x-relay headers are handled at request time.
	if proxyType == "vercel" || proxyType == "cloudflare" || proxyType == "deno" {
		return h.Client
	}

	// Unknown proxy types must never silently bypass a strict proxy policy.
	if strictProxy {
		proxyErr := fmt.Errorf("strict proxy configuration uses unsupported proxy type %q", proxyType)
		log.Error("proxy", "strict proxy enabled with unsupported proxy type; failing closed", "type", proxyType, "error", proxyErr)
		return &http.Client{
			Transport: failingRoundTripper{err: proxyErr},
			Timeout:   h.Client.Timeout,
		}
	}
	log.Warn("proxy", "unsupported proxy type; using direct client because strict proxy is disabled", "type", proxyType)
	return h.Client
}
