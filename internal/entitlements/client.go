package entitlements

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

var (
	ErrActivationCodeRequired = errors.New("activation code is required")
	ErrControlPlaneTransport  = errors.New("control plane transport is required")
	ErrRenewalLicenseChanged  = errors.New("renewal returned a different license id")
	ErrRenewalNonceMissing    = errors.New("cached lease nonce is missing")
	ErrBuildChannelRequired   = errors.New("license build channel is required")
	ErrBuildIDRequired        = errors.New("license build id is required for this channel")
)

// ActivationRequest is safe to send to the licensing control plane. The
// activation code is transient request material and must never be persisted by
// RuntimeStore or emitted to logs.
type ActivationRequest struct {
	ActivationCode        string `json:"activation_code"`
	InstallationID        string `json:"installation_id"`
	InstallationPublicKey string `json:"installation_public_key"`
	Platform              string `json:"platform"`
	Arch                  string `json:"arch"`
	AppVersion            string `json:"app_version,omitempty"`
	BuildChannel          string `json:"build_channel"`
	BuildID               string `json:"build_id,omitempty"`
	ProofVersion          int    `json:"proof_version"`
	ProofSignature        string `json:"proof_signature"`
}

// RenewalRequest identifies the already-verified license/installation without
// resending an activation code.
type RenewalRequest struct {
	LicenseID      string `json:"license_id"`
	InstallationID string `json:"installation_id"`
	CurrentNonce   string `json:"current_nonce"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	AppVersion     string `json:"app_version,omitempty"`
	BuildChannel   string `json:"build_channel"`
	BuildID        string `json:"build_id,omitempty"`
	ProofVersion   int    `json:"proof_version"`
	ProofSignature string `json:"proof_signature"`
}

// ReleaseRequest authorizes self-service release of the current installation.
// It is signed by the installation proof key and bound to the current lease nonce.
type ReleaseRequest struct {
	LicenseID      string `json:"license_id"`
	InstallationID string `json:"installation_id"`
	CurrentNonce   string `json:"current_nonce"`
	ProofVersion   int    `json:"proof_version"`
	ProofSignature string `json:"proof_signature"`
}

// LeaseResponse is returned by activate/renew. Lease must contain the raw
// signed JSON envelope consumed by VerifySignedLease.
type LeaseResponse struct {
	Lease               []byte
	ServerTime          time.Time
	RenewalAfterSeconds int64
}

// ControlPlaneTransport is deliberately provider-neutral. Production may use
// HTTP, Workers/D1 or another backend without changing entitlement logic.
type ControlPlaneTransport interface {
	Activate(context.Context, ActivationRequest) (LeaseResponse, error)
	Renew(context.Context, RenewalRequest) (LeaseResponse, error)
	Release(context.Context, ReleaseRequest) error
}

type ClientOptions struct {
	Store      *RuntimeStore
	Transport  ControlPlaneTransport
	Keys       KeyRing
	Build      BuildIdentity
	Platform   string
	Arch       string
	AppVersion string
	Now        func() time.Time
}

type Client struct {
	store      *RuntimeStore
	transport  ControlPlaneTransport
	keys       KeyRing
	build      BuildIdentity
	platform   string
	arch       string
	appVersion string
	now        func() time.Time
}

type ClientResult struct {
	Evaluation   *LeaseEvaluation
	RenewalAfter time.Duration
	ServerTime   time.Time
}


func ValidateLicensedBuildIdentity(build BuildIdentity) error {
	channel := strings.ToLower(strings.TrimSpace(build.Channel))
	if channel == "" {
		return ErrBuildChannelRequired
	}
	switch channel {
	case "beta", "stable", "release", "production", "prod":
		if strings.TrimSpace(build.ID) == "" {
			return ErrBuildIDRequired
		}
	}
	return nil
}

func NewClient(options ClientOptions) (*Client, error) {
	if err := ValidateLicensedBuildIdentity(options.Build); err != nil {
		return nil, err
	}
	if options.Store == nil {
		return nil, errors.New("entitlements.NewClient: store is required")
	}
	if options.Transport == nil {
		return nil, ErrControlPlaneTransport
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	platform := strings.TrimSpace(options.Platform)
	if platform == "" {
		platform = runtime.GOOS
	}
	arch := strings.TrimSpace(options.Arch)
	if arch == "" {
		arch = runtime.GOARCH
	}
	return &Client{
		store:      options.Store,
		transport:  options.Transport,
		keys:       options.Keys,
		build:      options.Build,
		platform:   platform,
		arch:       arch,
		appVersion: strings.TrimSpace(options.AppVersion),
		now:        now,
	}, nil
}

func (c *Client) Activate(ctx context.Context, activationCode string) (*ClientResult, error) {
	code := strings.TrimSpace(activationCode)
	if code == "" {
		return nil, ErrActivationCodeRequired
	}

	identity, err := c.store.InstallationIdentity()
	if err != nil {
		return nil, err
	}
	publicKey, err := encodeInstallationPublicKey(identity.PublicKey)
	if err != nil {
		return nil, err
	}

	request := ActivationRequest{
		ActivationCode:        code,
		InstallationID:        identity.ID,
		InstallationPublicKey: publicKey,
		Platform:              c.platform,
		Arch:                  c.arch,
		AppVersion:            c.appVersion,
		BuildChannel:          c.build.Channel,
		BuildID:               c.build.ID,
		ProofVersion:          ActivationProofVersion,
	}
	request.ProofSignature, err = signActivationProof(identity.PrivateKey, request)
	if err != nil {
		return nil, err
	}

	response, err := c.transport.Activate(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("entitlements.Client.Activate: %w", err)
	}

	return c.acceptLease(response, identity.ID, "")
}

func (c *Client) Renew(ctx context.Context) (*ClientResult, error) {
	identity, err := c.store.InstallationIdentity()
	if err != nil {
		return nil, err
	}
	current, err := c.verifyCachedLease()
	if err != nil {
		return nil, err
	}
	if current.Lease.InstallationID != identity.ID {
		return nil, ErrWrongInstallation
	}
	if strings.TrimSpace(current.Lease.Nonce) == "" {
		return nil, ErrRenewalNonceMissing
	}

	request := RenewalRequest{
		LicenseID:      current.Lease.LicenseID,
		InstallationID: current.Lease.InstallationID,
		CurrentNonce:   current.Lease.Nonce,
		Platform:       c.platform,
		Arch:           c.arch,
		AppVersion:     c.appVersion,
		BuildChannel:   c.build.Channel,
		BuildID:        c.build.ID,
		ProofVersion:   RenewalProofVersion,
	}
	request.ProofSignature, err = signRenewalProof(identity.PrivateKey, request)
	if err != nil {
		return nil, err
	}

	response, err := c.transport.Renew(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("entitlements.Client.Renew: %w", err)
	}

	return c.acceptLease(response, current.Lease.InstallationID, current.Lease.LicenseID)
}

func (c *Client) Release(ctx context.Context) error {
	identity, err := c.store.InstallationIdentity()
	if err != nil {
		return err
	}
	current, err := c.verifyCachedLease()
	if err != nil {
		return err
	}
	if current.Lease.InstallationID != identity.ID {
		return ErrWrongInstallation
	}
	if strings.TrimSpace(current.Lease.Nonce) == "" {
		return ErrRenewalNonceMissing
	}

	request := ReleaseRequest{
		LicenseID:      current.Lease.LicenseID,
		InstallationID: current.Lease.InstallationID,
		CurrentNonce:   current.Lease.Nonce,
		ProofVersion:   ReleaseProofVersion,
	}
	request.ProofSignature, err = signReleaseProof(identity.PrivateKey, request)
	if err != nil {
		return err
	}
	if err := c.transport.Release(ctx, request); err != nil {
		return fmt.Errorf("entitlements.Client.Release: %w", err)
	}
	return c.store.ClearLease()
}

func (c *Client) verifyCachedLease() (*LeaseEvaluation, error) {
	installationID, err := c.store.InstallationID()
	if err != nil {
		return nil, err
	}
	lastTrusted, err := c.store.LastTrustedTime()
	if err != nil {
		return nil, err
	}
	raw, err := c.store.LoadLease()
	if err != nil {
		return nil, err
	}

	return VerifySignedLease(raw, c.keys, VerificationContext{
		Now:             c.now().UTC(),
		LastTrustedTime: lastTrusted,
		InstallationID:  installationID,
		Build:           c.build,
	})
}

func (c *Client) acceptLease(response LeaseResponse, installationID, expectedLicenseID string) (*ClientResult, error) {
	if len(response.Lease) == 0 {
		return nil, ErrMalformedLease
	}

	lastTrusted, err := c.store.LastTrustedTime()
	if err != nil {
		return nil, err
	}

	localNow := c.now().UTC()
	verifyNow := localNow
	if !response.ServerTime.IsZero() && response.ServerTime.UTC().After(verifyNow) {
		verifyNow = response.ServerTime.UTC()
	}

	evaluation, err := VerifySignedLease(response.Lease, c.keys, VerificationContext{
		Now:             verifyNow,
		LastTrustedTime: lastTrusted,
		InstallationID:  installationID,
		Build:           c.build,
	})
	if err != nil {
		return nil, err
	}
	if expectedLicenseID != "" && evaluation.Lease.LicenseID != expectedLicenseID {
		return nil, ErrRenewalLicenseChanged
	}

	trustedCandidate := localNow
	if !response.ServerTime.IsZero() && response.ServerTime.UTC().After(trustedCandidate) {
		trustedCandidate = response.ServerTime.UTC()
	}
	if _, err := c.store.AdvanceTrustedTime(trustedCandidate); err != nil {
		return nil, err
	}
	if err := c.store.SaveLease(response.Lease); err != nil {
		return nil, err
	}

	renewalAfter := time.Duration(0)
	if response.RenewalAfterSeconds > 0 {
		renewalAfter = time.Duration(response.RenewalAfterSeconds) * time.Second
	}
	return &ClientResult{
		Evaluation:   evaluation,
		RenewalAfter: renewalAfter,
		ServerTime:   response.ServerTime.UTC(),
	}, nil
}
