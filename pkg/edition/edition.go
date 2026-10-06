package edition

import "time"

// Capability is a stable product feature gate shared by Community and
// commercial editions. Public code should depend on capabilities rather than
// payment processors, product keys, plan names or private implementations.
type Capability string

const (
	CapabilityAdvancedRouting    Capability = "fabric.advanced_routing"
	CapabilityMultiAccountPolicy Capability = "fabric.multi_account_policy"
	CapabilityIntentProfiles     Capability = "fabric.intent_profiles"
	CapabilityAdvancedTelemetry  Capability = "telemetry.advanced"
	CapabilityBudgetPolicy       Capability = "policy.budget"
	CapabilityQuotaExpiry        Capability = "policy.quota_expiry_preference"
)

// Status is intentionally safe for UI/API exposure. Implementations must not
// place provider credentials, signing private keys, payment data or other
// secrets in this structure.
type Status struct {
	Mode               string       `json:"mode"`
	State              string       `json:"state,omitempty"`
	Channel            string       `json:"channel,omitempty"`
	Plan               string       `json:"plan,omitempty"`
	LicenseID          string       `json:"licenseId,omitempty"`
	InstallationID     string       `json:"installationId,omitempty"`
	EntitlementVersion int          `json:"entitlementVersion,omitempty"`
	Subject            string       `json:"subject,omitempty"`
	ExpiresAt          *time.Time   `json:"expiresAt,omitempty"`
	GraceUntil         *time.Time   `json:"graceUntil,omitempty"`
	Features           []Capability `json:"features,omitempty"`
}

// Provider is the public edition boundary. A separate commercial module may
// implement this interface without importing any 9router internal package.
//
// Community is the fail-safe behavior: absent a valid provider, paid
// capabilities remain disabled.
type Provider interface {
	Enabled(Capability) bool
	Status() Status
}

// CommunityProvider is the default implementation shipped by the open source
// build. It intentionally enables no commercial capabilities.
type CommunityProvider struct{}

func (CommunityProvider) Enabled(Capability) bool { return false }

func (CommunityProvider) Status() Status {
	return Status{Mode: "community"}
}
