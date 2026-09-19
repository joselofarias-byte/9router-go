package discovery

import (
	"context"
	"time"

	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
)

// KiroStaticAdapter declares Kiro's known free social-OAuth model set.
// Kiro has no public catalog endpoint; models come from the data-plane
// registry. Declaring them here lets fabric-free see Kiro only when the
// user has an active Kiro account in providerConnections.
type KiroStaticAdapter struct{}

func NewKiroStaticAdapter() *KiroStaticAdapter { return &KiroStaticAdapter{} }

func (a *KiroStaticAdapter) SourceID() string { return "kiro-static" }

func (a *KiroStaticAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	_ = ctx
	models := providers.GetProviderModels("kiro")
	now := time.Now().UTC()
	candidates := make([]Candidate, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    "static: providers.GetProviderModels(\"kiro\")",
			Confidence:    0.9,
			RetrievalTime: now,
			ProviderID:    "kiro",
			ModelID:       model,
			UpstreamModel: model,
			PricingMode:   "free",
			Capabilities:  "{}",
		})
	}
	log.Info("discovery", "Kiro static free set declared", "candidates", len(candidates))
	return candidates, nil
}
