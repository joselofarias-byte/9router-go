package discovery

import (
	"context"
	"time"

	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
)

// KiroStaticAdapter surfaces Kiro's free, social-OAuth-login model set as
// fabric-free candidates. Kiro has no discoverable catalog endpoint — its
// models are a fixed set already known to the data plane (see
// providers.ModelsForProvider("kiro")) — so this adapter declares that known
// set rather than fetching one over the network. It is the missing link that
// let an already-integrated free provider (OAuth import, executor, risk
// profile all exist in internal/providers and internal/handlers/oauth) reach
// the Fabric registry: without it, Kiro's free capacity was never a
// candidate for the "fabric-free" pool even though the account/provider risk
// engine already classified it as an account-sensitive surface.
//
// Actual usability still depends on the user having an active Kiro OAuth
// connection — routing.SelectCandidates only returns a candidate when a
// matching active registry.Account exists, so declaring the model set here
// does not, by itself, route traffic anywhere the user hasn't connected.
type KiroStaticAdapter struct{}

func NewKiroStaticAdapter() *KiroStaticAdapter {
	return &KiroStaticAdapter{}
}

func (a *KiroStaticAdapter) SourceID() string {
	return "kiro-static"
}

func (a *KiroStaticAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	models := providers.ModelsForProvider("kiro")
	now := time.Now().UTC()
	candidates := make([]Candidate, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    "static: providers.ModelsForProvider(\"kiro\")",
			Confidence:    0.9,
			RetrievalTime: now,
			ProviderID:    "kiro",
			ModelID:       model,
			UpstreamModel: model,
			PricingMode:   "free",
			Capabilities:  "{}", // verified separately by capability probes
		})
	}
	log.Info("discovery", "Kiro static free set declared", "candidates", len(candidates))
	return candidates, nil
}
