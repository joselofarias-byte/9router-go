package registry

import (
	"encoding/json"
	"time"
)

// Provider represents a canonical provider.
type Provider struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Model represents a canonical model.
type Model struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProviderModel represents a model offered by a specific provider.
type ProviderModel struct {
	ProviderID    string    `json:"providerId"`
	ModelID       string    `json:"modelId"`
	UpstreamModel string    `json:"upstreamModel"`
	PricingMode   string    `json:"pricingMode"` // "free", "free_tier", "paid", "unknown"
	CostMetadata  string    `json:"costMetadata"` // JSON blob for cost tracking
	Capabilities  string    `json:"capabilities"` // JSON blob of boolean capabilities
	IsActive      bool      `json:"isActive"`
	// Source is the discovery adapter SourceID that produced this entry
	// (e.g. "cline-free", "unorouter"). Used by the orchestrator to
	// reconcile staleness: an entry only gets deactivated when the adapter
	// that owns it successfully re-syncs and no longer reports it, never
	// when an unrelated adapter runs or a fetch merely fails.
	Source        string    `json:"source,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Account represents a provider account (credentials).
type Account struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"providerId"`
	// Note: AuthData is intentionally omitted from the Registry schema to prevent
	// plaintext credential exposure in registry JSON snapshots. Data Plane
	// handles credential injection securely from the underlying DB rows.
	IsActive   bool      `json:"isActive"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// RegistryState represents the entire active registry state in memory.
type RegistryState struct {
	Providers      map[string]*Provider           `json:"providers"`
	Models         map[string]*Model              `json:"models"`
	ProviderModels map[string]map[string]*ProviderModel `json:"providerModels"`
	Accounts       map[string]*Account            `json:"accounts"`
}

// Snapshot represents a versioned snapshot of the registry state.
type Snapshot struct {
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Reason    string    `json:"reason"`
	Checksum  string    `json:"checksum"`
	Status    string    `json:"status"` // "candidate", "active", "last_known_good"
	Payload   string    `json:"payload"` // Serialized RegistryState
}

// ToJSON converts RegistryState to a canonical JSON string.
func (s *RegistryState) ToJSON() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FromJSON parses a JSON string into a RegistryState.
func FromJSON(data string) (*RegistryState, error) {
	var state RegistryState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, err
	}
	return &state, nil
}
