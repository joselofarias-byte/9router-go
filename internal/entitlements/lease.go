package entitlements

import (
	"crypto/ed25519"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CurrentLeaseVersion = 1
	DefaultLeaseClockSkew = 5 * time.Minute
)

var (
	ErrMalformedLease             = errors.New("malformed entitlement lease")
	ErrFutureLease                = errors.New("entitlement lease issued in the future")
	ErrExpiredLease               = errors.New("entitlement lease expired")
	ErrUnknownKey                 = errors.New("unknown entitlement signing key")
	ErrWrongInstallation          = errors.New("entitlement lease belongs to another installation")
	ErrWrongBuildChannel          = errors.New("entitlement lease is not valid for this build channel")
	ErrWrongBuildID               = errors.New("entitlement lease is not valid for this build")
	ErrBuildExpired               = errors.New("build is no longer Pro-capable")
	ErrClockRollback              = errors.New("local clock moved behind last trusted time")
	ErrInvalidVerificationContext = errors.New("invalid entitlement verification context")
)

// Plan is intentionally open-ended: the verifier treats a plan as signed
// metadata and gates behavior by capabilities, not by hard-coded SKU names.
type Plan string

const (
	PlanFree      Plan = "free"
	PlanBetaPro   Plan = "beta_pro"
	PlanSupporter Plan = "supporter"
	PlanPro       Plan = "pro"
	PlanBusiness  Plan = "business"
)

type LeaseState string

const (
	LeaseStateActive LeaseState = "active"
	LeaseStateGrace  LeaseState = "grace"
)

// Lease is a short-lived, offline-verifiable entitlement lease. It binds a
// license to one random installation UUID and one build channel. A control
// plane may issue separate leases with the same LicenseID to different
// installations up to the configured seat/installation limit.
type Lease struct {
	Version            int          `json:"version"`
	KeyID              string       `json:"kid"`
	LicenseID          string       `json:"license_id"`
	InstallationID     string       `json:"installation_id"`
	Plan               Plan         `json:"plan"`
	EntitlementVersion int          `json:"entitlement_version"`
	Entitlements       []Capability `json:"entitlements"`
	IssuedAt           time.Time    `json:"issued_at"`
	ExpiresAt          time.Time    `json:"expires_at"`
	GraceUntil         time.Time    `json:"grace_until"`
	BuildChannel       string       `json:"build_channel"`
	BuildID            string       `json:"build_id,omitempty"`
	Nonce              string       `json:"nonce"`
	Signature          string       `json:"signature"`
}

type leaseSigningPayload struct {
	Version            int          `json:"version"`
	KeyID              string       `json:"kid"`
	LicenseID          string       `json:"license_id"`
	InstallationID     string       `json:"installation_id"`
	Plan               Plan         `json:"plan"`
	EntitlementVersion int          `json:"entitlement_version"`
	Entitlements       []Capability `json:"entitlements"`
	IssuedAt           time.Time    `json:"issued_at"`
	ExpiresAt          time.Time    `json:"expires_at"`
	GraceUntil         time.Time    `json:"grace_until"`
	BuildChannel       string       `json:"build_channel"`
	BuildID            string       `json:"build_id,omitempty"`
	Nonce              string       `json:"nonce"`
}

func (l *Lease) payload() leaseSigningPayload {
	if l == nil {
		return leaseSigningPayload{}
	}
	return leaseSigningPayload{
		Version:            l.Version,
		KeyID:              l.KeyID,
		LicenseID:          l.LicenseID,
		InstallationID:     l.InstallationID,
		Plan:               l.Plan,
		EntitlementVersion: l.EntitlementVersion,
		Entitlements:       append([]Capability(nil), l.Entitlements...),
		IssuedAt:           l.IssuedAt.UTC(),
		ExpiresAt:          l.ExpiresAt.UTC(),
		GraceUntil:         l.GraceUntil.UTC(),
		BuildChannel:       l.BuildChannel,
		BuildID:            l.BuildID,
		Nonce:              l.Nonce,
	}
}

func canonicalLeasePayload(l *Lease) ([]byte, error) {
	payload, err := json.Marshal(l.payload())
	if err != nil {
		return nil, fmt.Errorf("entitlements.canonicalLeasePayload: %w", err)
	}
	return payload, nil
}

// KeyRing supports signing-key rotation through the signed kid field.
type KeyRing map[string]ed25519.PublicKey

// ParseKeyRing decodes base64 public keys. Private signing keys never belong in
// this package or in the public repository.
func ParseKeyRing(encoded map[string]string) (KeyRing, error) {
	out := make(KeyRing, len(encoded))
	for kid, encodedKey := range encoded {
		kid = strings.TrimSpace(kid)
		if kid == "" {
			return nil, fmt.Errorf("%w: empty kid", ErrInvalidPublicKey)
		}
		key, err := ParsePublicKey(encodedKey)
		if err != nil {
			return nil, fmt.Errorf("kid %q: %w", kid, err)
		}
		out[kid] = append(ed25519.PublicKey(nil), key...)
	}
	return out, nil
}

type BuildIdentity struct {
	Channel         string
	ID              string
	ProCapableUntil time.Time
}

// VerificationContext carries installation/build identity plus trusted clock
// information. InstallationID should be a random persisted UUID, not a hardware
// identifier.
type VerificationContext struct {
	Now             time.Time
	LastTrustedTime time.Time
	InstallationID  string
	Build           BuildIdentity
	ClockSkew       time.Duration
}

type LeaseEvaluation struct {
	Lease            *Lease
	State            LeaseState
	ProCapableUntil  *time.Time
}

// VerifySignedLease validates signature, key rotation, installation binding,
// build constraints, expiry/grace and a simple rollback guard. It does not call
// a server and is therefore suitable for cached/offline startup.
func VerifySignedLease(raw []byte, keys KeyRing, ctx VerificationContext) (*LeaseEvaluation, error) {
	if ctx.Now.IsZero() ||
		strings.TrimSpace(ctx.InstallationID) == "" ||
		strings.TrimSpace(ctx.Build.Channel) == "" ||
		ctx.ClockSkew < 0 {
		return nil, ErrInvalidVerificationContext
	}

	skew := ctx.ClockSkew
	if skew == 0 {
		skew = DefaultLeaseClockSkew
	}
	now := ctx.Now.UTC()
	if !ctx.LastTrustedTime.IsZero() && now.Add(skew).Before(ctx.LastTrustedTime.UTC()) {
		return nil, ErrClockRollback
	}

	var lease Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedLease, err)
	}
	if err := validateLeaseShape(&lease); err != nil {
		return nil, err
	}

	key, ok := keys[lease.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return nil, ErrUnknownKey
	}
	signature, err := base64.StdEncoding.DecodeString(lease.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, ErrInvalidSignature
	}
	payload, err := canonicalLeasePayload(&lease)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(key, payload, signature) {
		return nil, ErrInvalidSignature
	}

	if lease.IssuedAt.After(now.Add(skew)) {
		return nil, ErrFutureLease
	}
	if lease.InstallationID != strings.TrimSpace(ctx.InstallationID) {
		return nil, ErrWrongInstallation
	}
	if lease.BuildChannel != strings.TrimSpace(ctx.Build.Channel) {
		return nil, ErrWrongBuildChannel
	}
	if lease.BuildID != "" && lease.BuildID != strings.TrimSpace(ctx.Build.ID) {
		return nil, ErrWrongBuildID
	}

	var proCapableUntil *time.Time
	if !ctx.Build.ProCapableUntil.IsZero() {
		t := ctx.Build.ProCapableUntil.UTC()
		proCapableUntil = &t
		if !now.Before(t) {
			return nil, ErrBuildExpired
		}
	}
	if !now.Before(lease.GraceUntil) {
		return nil, ErrExpiredLease
	}

	lease.Entitlements = dedupeCapabilities(lease.Entitlements)
	state := LeaseStateActive
	if !now.Before(lease.ExpiresAt) {
		state = LeaseStateGrace
	}
	return &LeaseEvaluation{
		Lease:           &lease,
		State:           state,
		ProCapableUntil: proCapableUntil,
	}, nil
}

func validateLeaseShape(lease *Lease) error {
	if lease == nil ||
		lease.Version != CurrentLeaseVersion ||
		strings.TrimSpace(lease.KeyID) == "" ||
		strings.TrimSpace(lease.LicenseID) == "" ||
		strings.TrimSpace(lease.InstallationID) == "" ||
		strings.TrimSpace(string(lease.Plan)) == "" ||
		lease.EntitlementVersion <= 0 ||
		lease.IssuedAt.IsZero() ||
		lease.ExpiresAt.IsZero() ||
		lease.GraceUntil.IsZero() ||
		!lease.ExpiresAt.After(lease.IssuedAt) ||
		lease.GraceUntil.Before(lease.ExpiresAt) ||
		strings.TrimSpace(lease.BuildChannel) == "" ||
		strings.TrimSpace(lease.Nonce) == "" ||
		strings.TrimSpace(lease.Signature) == "" {
		return ErrMalformedLease
	}
	return nil
}

// LeaseProvider adapts a verified lease to the generic Provider interface. It
// rechecks expiry/grace/build lifetime on every read, so a long-running process
// falls back to Community without restart.
type LeaseProvider struct {
	lease           *Lease
	enabled         map[Capability]struct{}
	now             func() time.Time
	proCapableUntil *time.Time
}

func NewLeaseProvider(evaluation *LeaseEvaluation) *LeaseProvider {
	p := &LeaseProvider{
		enabled: make(map[Capability]struct{}),
		now:     time.Now,
	}
	if evaluation == nil || evaluation.Lease == nil {
		return p
	}

	snapshot := *evaluation.Lease
	snapshot.Entitlements = append([]Capability(nil), evaluation.Lease.Entitlements...)
	p.lease = &snapshot
	for _, capability := range snapshot.Entitlements {
		p.enabled[capability] = struct{}{}
	}
	if evaluation.ProCapableUntil != nil {
		t := evaluation.ProCapableUntil.UTC()
		p.proCapableUntil = &t
	}
	return p
}

func (p *LeaseProvider) Enabled(capability Capability) bool {
	if _, ok := p.currentState(); !ok {
		return false
	}
	_, ok := p.enabled[capability]
	return ok
}

func (p *LeaseProvider) Status() Status {
	state, ok := p.currentState()
	if !ok {
		return Status{Mode: "community"}
	}
	expiresAt := p.lease.ExpiresAt
	graceUntil := p.lease.GraceUntil
	return Status{
		Mode:               "licensed",
		State:              string(state),
		Channel:            p.lease.BuildChannel,
		Plan:               p.lease.Plan,
		LicenseID:          p.lease.LicenseID,
		InstallationID:     p.lease.InstallationID,
		EntitlementVersion: p.lease.EntitlementVersion,
		ExpiresAt:          &expiresAt,
		GraceUntil:         &graceUntil,
		Features:           append([]Capability(nil), p.lease.Entitlements...),
	}
}

func (p *LeaseProvider) currentState() (LeaseState, bool) {
	if p == nil || p.lease == nil || p.now == nil {
		return "", false
	}
	now := p.now().UTC()
	if p.proCapableUntil != nil && !now.Before(*p.proCapableUntil) {
		return "", false
	}
	if !now.Before(p.lease.GraceUntil) {
		return "", false
	}
	if now.Before(p.lease.ExpiresAt) {
		return LeaseStateActive, true
	}
	return LeaseStateGrace, true
}
