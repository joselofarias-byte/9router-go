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
// model is explicitly labelled free, except mimo-free whose whole endpoint is
// the product's dedicated free lane.
type BuiltinFreeAdapter struct{}

func NewBuiltinFreeAdapter() *BuiltinFreeAdapter { return &BuiltinFreeAdapter{} }

func (a *BuiltinFreeAdapter) SourceID() string { return "builtin-free" }

func (a *BuiltinFreeAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	_ = ctx
	now := time.Now().UTC()
	var out []Candidate
	for providerID, cfg := range providers.KnownProviders {
		if !cfg.NoAuth {
			continue
		}
		for _, modelID := range providers.ProviderModels[providerID] {
			if providerID != "mimo-free" && !explicitFreeModelID(modelID) {
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
		if !cfg.NoAuth {
			continue
		}
		for _, modelID := range providers.ProviderModels[providerID] {
			if providerID == "mimo-free" || explicitFreeModelID(modelID) {
				ids = append(ids, providerID)
				break
			}
		}
	}
	return ids
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
