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
)

// ActivationRequest is safe to send to the licensing control plane. The
// activation code is transient request material and must never be persisted by
// RuntimeStore or emitted to logs.
type ActivationRequest struct {
	ActivationCode string `json:"activation_code"`
	InstallationID string `json:"installation_id"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	AppVersion     string `json:"app_version,omitempty"`
	BuildChannel   string `json:"build_channel"`
	BuildID        string `json:"build_id,omitempty"`
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

func NewClient(options ClientOptions) (*Client, error) {
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

	installationID, err := c.store.InstallationID()
	if err != nil {
		return nil, err
	}

	response, err := c.transport.Activate(ctx, ActivationRequest{
		ActivationCode: code,
		InstallationID: installationID,
		Platform:       c.platform,
		Arch:           c.arch,
		AppVersion:     c.appVersion,
		BuildChannel:   c.build.Channel,
		BuildID:        c.build.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("entitlements.Client.Activate: %w", err)
	}

	return c.acceptLease(response, installationID, "")
}

func (c *Client) Renew(ctx context.Context) (*ClientResult, error) {
	current, err := c.verifyCachedLease()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(current.Lease.Nonce) == "" {
		return nil, ErrRenewalNonceMissing
	}

	response, err := c.transport.Renew(ctx, RenewalRequest{
		LicenseID:      current.Lease.LicenseID,
		InstallationID: current.Lease.InstallationID,
		CurrentNonce:   current.Lease.Nonce,
		Platform:       c.platform,
		Arch:           c.arch,
		AppVersion:     c.appVersion,
		BuildChannel:   c.build.Channel,
		BuildID:        c.build.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("entitlements.Client.Renew: %w", err)
	}

	return c.acceptLease(response, current.Lease.InstallationID, current.Lease.LicenseID)
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
