package entitlements

import "time"

// Capability is a stable feature gate name. The routing layer should depend on
// capabilities, not on a payment processor or a subscription SKU.
type Capability string

const (
	CapabilityAdvancedRouting    Capability = "fabric.advanced_routing"
	CapabilityMultiAccountPolicy Capability = "fabric.multi_account_policy"
	CapabilityIntentProfiles     Capability = "fabric.intent_profiles"
	CapabilityAdvancedTelemetry  Capability = "telemetry.advanced"
	CapabilityBudgetPolicy       Capability = "policy.budget"
	CapabilityQuotaExpiry        Capability = "policy.quota_expiry_preference"
)

// Status is safe to expose to the UI. It contains no private signing material
// and no provider credentials.
type Status struct {
	Mode               string       `json:"mode"`
	State              string       `json:"state,omitempty"`
	Channel            string       `json:"channel,omitempty"`
	Plan               Plan         `json:"plan,omitempty"`
	LicenseID          string       `json:"licenseId,omitempty"`
	InstallationID     string       `json:"installationId,omitempty"`
	EntitlementVersion int          `json:"entitlementVersion,omitempty"`
	Subject            string       `json:"subject,omitempty"`
	ExpiresAt          *time.Time   `json:"expiresAt,omitempty"`
	GraceUntil         *time.Time   `json:"graceUntil,omitempty"`
	Features           []Capability `json:"features,omitempty"`
}

// Provider answers feature-capability questions. Community remains the
// fail-safe default when no paid/beta entitlement provider is installed.
type Provider interface {
	Enabled(Capability) bool
	Status() Status
}

// CommunityProvider never enables Pro capabilities.
type CommunityProvider struct{}

func (CommunityProvider) Enabled(Capability) bool { return false }

func (CommunityProvider) Status() Status {
	return Status{Mode: "community"}
}

// LicenseProvider exposes a verified license as capabilities.
type LicenseProvider struct {
	license *License
	enabled map[Capability]struct{}
	now     func() time.Time
}

func NewLicenseProvider(license *License) *LicenseProvider {
	p := &LicenseProvider{
		now:     time.Now,
		enabled: make(map[Capability]struct{}),
	}
	if license == nil {
		return p
	}
	// Keep the verified input immutable for the lifetime of this provider.
	snapshot := *license
	snapshot.Features = append([]Capability(nil), license.Features...)
	p.license = &snapshot
	for _, feature := range snapshot.Features {
		p.enabled[feature] = struct{}{}
	}
	return p
}

func (p *LicenseProvider) Enabled(capability Capability) bool {
	if !p.active() {
		return false
	}
	_, ok := p.enabled[capability]
	return ok
}

func (p *LicenseProvider) Status() Status {
	if !p.active() {
		return Status{Mode: "community"}
	}
	expiresAt := p.license.ExpiresAt
	features := append([]Capability(nil), p.license.Features...)
	return Status{
		Mode:      "licensed",
		Channel:   p.license.Channel,
		LicenseID: p.license.LicenseID,
		Subject:   p.license.Subject,
		ExpiresAt: &expiresAt,
		Features:  features,
	}
}

// active rechecks expiry on every capability/status read so a running process
// degrades to Community without requiring a restart or license reload.
func (p *LicenseProvider) active() bool {
	if p == nil || p.license == nil || p.now == nil {
		return false
	}
	return p.now().Before(p.license.ExpiresAt)
}
