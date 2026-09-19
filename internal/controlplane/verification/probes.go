package verification

import (
	"context"
	"time"

	"9router/proxy/internal/providers"
)

// ProbeResult represents the outcome of an advanced verification probe.
type ProbeResult struct {
	Timestamp      time.Time
	ProviderID     string
	ModelID        string
	AccountID      string

	Success        bool
	LatencyMs      int
	TTFTMs         int
	TokensPerSec   float64

	ErrorCategory  providers.ErrorCategory
	ErrorMessage   string

	CapabilitiesVerified map[string]bool // dynamic validation of streaming/json/tools
	FingerprintMatches   bool            // true if upstream model ID / behavior matches expectations
}

// Prober defines the interface for executing advanced probes outside the hot path.
type Prober interface {
	// Probe determines if a candidate model/account is healthy and verifies capabilities.
	Probe(ctx context.Context, providerID, modelID, accountID string) (ProbeResult, error)
}
