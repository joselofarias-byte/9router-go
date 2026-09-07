package discovery

import (
	"context"
)

// RelayinAdapter scaffolds discovery for Relayin API
type RelayinAdapter struct{}

func (a *RelayinAdapter) SourceID() string { return "relayin" }

func (a *RelayinAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	// UNVERIFIED EXTERNAL INTEGRATION
	// Requires: Relayin API credentials or public endpoints documentation.
	return []Candidate{}, nil
}

// ModelRadarAdapter scaffolds discovery for ModelRadar API
type ModelRadarAdapter struct{}

func (a *ModelRadarAdapter) SourceID() string { return "modelradar" }

func (a *ModelRadarAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	// UNVERIFIED EXTERNAL INTEGRATION
	// Requires: ModelRadar API documentation or endpoint access.
	return []Candidate{}, nil
}

// DataAPIAdapter scaffolds discovery for MODELOC Data API
type DataAPIAdapter struct{}

func (a *DataAPIAdapter) SourceID() string { return "modeloc_data_api" }

func (a *DataAPIAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	// UNVERIFIED EXTERNAL INTEGRATION
	// Requires: Access to the MODELOC API or specific configuration.
	return []Candidate{}, nil
}
