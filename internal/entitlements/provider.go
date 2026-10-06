package entitlements

import (
	"time"

	"9router/proxy/pkg/edition"
)

// Public product-edition contracts live in pkg/edition so a separate
// commercial repository can implement them without importing internal
// packages. These aliases preserve the existing internal API.
type Capability = edition.Capability

const (
	CapabilityAdvancedRouting    = edition.CapabilityAdvancedRouting
	CapabilityMultiAccountPolicy = edition.CapabilityMultiAccountPolicy
	CapabilityIntentProfiles     = edition.CapabilityIntentProfiles
	CapabilityAdvancedTelemetry  = edition.CapabilityAdvancedTelemetry
	CapabilityBudgetPolicy       = edition.CapabilityBudgetPolicy
	CapabilityQuotaExpiry        = edition.CapabilityQuotaExpiry
)

type Status = edition.Status
type Provider = edition.Provider
type CommunityProvider = edition.CommunityProvider

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
