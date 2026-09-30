package discovery

import (
	"context"
	"time"
)

// Candidate represents a discovered provider/model offering.
type Candidate struct {
	SourceID      string
	Provenance    string
	Confidence    float64
	RetrievalTime time.Time

	ProviderID    string
	ModelID       string
	UpstreamModel string

	PricingMode  string // "free", "free_tier", "paid", "unknown"
	CostMetadata string // JSON blob
	Capabilities string // JSON blob

	RawData string // Original raw metadata
}

// Adapter defines the interface for all discovery sources.
type Adapter interface {
	Discover(ctx context.Context) ([]Candidate, error)
	SourceID() string
}

// ProviderScoper is an optional Adapter capability. After a successful
// Discover, including an empty catalog, models this source owns for these
// provider ids that are absent from the new catalog are deactivated. That
// stops free routing from keeping a model Fabric no longer classifies as free.
type ProviderScoper interface {
	ScopedProviderIDs() []string
}
