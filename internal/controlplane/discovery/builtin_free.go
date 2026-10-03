package discovery

import (
	"context"
	"strings"
	"time"

	"9router/proxy/internal/providers"
)

// BuiltinFreeAdapter exposes the credential-free models that are already
// compiled into 9router's provider registry to Fabric's free-only policy.
// These are not guessed from pricing: the provider itself is NoAuth and the
// model is explicitly labelled free. Network discovery can add additional
// currently-free lanes after startup.
type BuiltinFreeAdapter struct{}

func NewBuiltinFreeAdapter() *BuiltinFreeAdapter { return &BuiltinFreeAdapter{} }

func (a *BuiltinFreeAdapter) SourceID() string { return "builtin-free" }

func (a *BuiltinFreeAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	_ = ctx
	now := time.Now().UTC()
	var out []Candidate
	for providerID, cfg := range providers.KnownProviders {
		if !cfg.NoAuth || !noAuthFreeProviderReady(providerID) {
			continue
		}
		for _, modelID := range providers.ProviderModels[providerID] {
			// Fabric's chat pool must never ingest media/special-service models.
			// In particular, OpenCode's jev-1.13-free is kind=systemone and
			// answers /v1/systemone rather than /v1/chat/completions.
			if !isChatProviderModel(providerID, modelID) {
				continue
			}
			if !explicitFreeModelID(modelID) {
				continue
			}
			out = append(out, Candidate{
				SourceID:      a.SourceID(),
				Provenance:    "compiled 9router provider registry",
				Confidence:    1.0,
				RetrievalTime: now,
				ProviderID:    providerID,
				ModelID:       canonicalDiscoveryModelID(modelID),
				UpstreamModel: modelID,
				PricingMode:   "free",
			})
		}
	}
	return out, nil
}

func (a *BuiltinFreeAdapter) ScopedProviderIDs() []string {
	var ids []string
	for providerID, cfg := range providers.KnownProviders {
		if !cfg.NoAuth || !noAuthFreeProviderReady(providerID) {
			continue
		}
		for _, modelID := range providers.ProviderModels[providerID] {
			if !isChatProviderModel(providerID, modelID) {
				continue
			}
			if explicitFreeModelID(modelID) {
				ids = append(ids, providerID)
				break
			}
		}
	}
	return ids
}



func noAuthFreeProviderReady(providerID string) bool {
	// Only expose zero-credential providers whose current data plane knows how
	// to satisfy a chat request. opencode-zen is intentionally held back on
	// this branch until its multi-transport executor (/chat, /messages,
	// /responses) is reconciled from upstream; otherwise Fabric advertises
	// free models that the generic forwarder cannot fingerprint/route safely.
	switch providerID {
	case "opencode", "llamacpp":
		return true
	default:
		return false
	}
}

func isChatProviderModel(providerID, modelID string) bool {
	kinds := providers.ProviderModelKinds[providerID]
	if len(kinds) == 0 {
		return true
	}
	for _, id := range []string{strings.TrimSpace(modelID), canonicalDiscoveryModelID(modelID)} {
		if id == "" {
			continue
		}
		if kind := strings.TrimSpace(kinds[id]); kind != "" {
			return false
		}
	}
	return true
}

func canonicalDiscoveryModelID(modelID string) string {
	base := strings.ToLower(strings.TrimSpace(modelID))
	if idx := strings.Index(base, "/"); idx != -1 {
		base = base[idx+1:]
	}
	if idx := strings.Index(base, ":"); idx != -1 {
		base = base[:idx]
	}
	return base
}

// explicitFreeModelID intentionally recognizes only unambiguous free markers.
// Names such as "freeform" do not qualify.
func explicitFreeModelID(modelID string) bool {
	id := strings.ToLower(strings.TrimSpace(modelID))
	return strings.HasSuffix(id, ":free") ||
		strings.HasSuffix(id, "/free") ||
		strings.HasSuffix(id, "-free") ||
		strings.Contains(id, "-free-")
}
