package entitlements

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

const (
	RenewalProofVersion    = 1
	ActivationProofVersion = 1
	ReleaseProofVersion    = 1
)

var (
	ErrInvalidRenewalProof    = errors.New("invalid renewal proof")
	ErrInvalidActivationProof = errors.New("invalid activation proof")
	ErrInvalidReleaseProof    = errors.New("invalid release proof")
)

type activationProofPayload struct {
	ProofVersion          int    `json:"proof_version"`
	Action                string `json:"action"`
	ActivationCodeHash    string `json:"activation_code_hash"`
	InstallationID        string `json:"installation_id"`
	InstallationPublicKey string `json:"installation_public_key"`
	Platform              string `json:"platform"`
	Arch                  string `json:"arch"`
	AppVersion            string `json:"app_version,omitempty"`
	BuildChannel          string `json:"build_channel"`
	BuildID               string `json:"build_id,omitempty"`
}

func activationCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func canonicalActivationProofPayload(request ActivationRequest) ([]byte, error) {
	payload := activationProofPayload{
		ProofVersion:          request.ProofVersion,
		Action:                "activate",
		ActivationCodeHash:    activationCodeHash(request.ActivationCode),
		InstallationID:        strings.TrimSpace(request.InstallationID),
		InstallationPublicKey: strings.TrimSpace(request.InstallationPublicKey),
		Platform:              strings.TrimSpace(request.Platform),
		Arch:                  strings.TrimSpace(request.Arch),
		AppVersion:            strings.TrimSpace(request.AppVersion),
		BuildChannel:          strings.TrimSpace(request.BuildChannel),
		BuildID:               strings.TrimSpace(request.BuildID),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("entitlements.canonicalActivationProofPayload: %w", err)
	}
	return raw, nil
}

func activationProofKeyBound(request ActivationRequest, publicKey ed25519.PublicKey) bool {
	encoded, err := encodeInstallationPublicKey(publicKey)
	if err != nil || encoded != strings.TrimSpace(request.InstallationPublicKey) {
		return false
	}
	return keyBackedInstallationMatches(request.InstallationID, publicKey)
}

// keyBackedInstallationMatches enforces the ik1_ installation id as a
// commitment to the verification key. Legacy UUID ids are not self-describing
// and stay bound to the key the caller looked up.
func keyBackedInstallationMatches(installationID string, publicKey ed25519.PublicKey) bool {
	installationID = strings.TrimSpace(installationID)
	if !strings.HasPrefix(installationID, installationIDPrefix) {
		return true
	}
	return len(publicKey) == ed25519.PublicKeySize && installationID == installationIDFromPublicKey(publicKey)
}

func signActivationProof(privateKey ed25519.PrivateKey, request ActivationRequest) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize || request.ProofVersion != ActivationProofVersion {
		return "", ErrInvalidActivationProof
	}
	payload, err := canonicalActivationProofPayload(request)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)), nil
}

// VerifyActivationProof verifies proof-of-possession for first activation.
// The activation code itself is not placed in the signed payload; only its
// SHA-256 digest is covered so the proof is domain-bound without duplicating
// the bearer secret.
func VerifyActivationProof(request ActivationRequest, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize ||
		request.ProofVersion != ActivationProofVersion ||
		strings.TrimSpace(request.ProofSignature) == "" ||
		!activationProofKeyBound(request, publicKey) {
		return ErrInvalidActivationProof
	}
	signature, err := base64.StdEncoding.DecodeString(request.ProofSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidActivationProof
	}
	payload, err := canonicalActivationProofPayload(request)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return ErrInvalidActivationProof
	}
	return nil
}

type releaseProofPayload struct {
	ProofVersion   int    `json:"proof_version"`
	Action         string `json:"action"`
	LicenseID      string `json:"license_id"`
	InstallationID string `json:"installation_id"`
	CurrentNonce   string `json:"current_nonce"`
}

func canonicalReleaseProofPayload(request ReleaseRequest) ([]byte, error) {
	payload := releaseProofPayload{
		ProofVersion:   request.ProofVersion,
		Action:         "release",
		LicenseID:      strings.TrimSpace(request.LicenseID),
		InstallationID: strings.TrimSpace(request.InstallationID),
		CurrentNonce:   strings.TrimSpace(request.CurrentNonce),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("entitlements.canonicalReleaseProofPayload: %w", err)
	}
	return raw, nil
}

func signReleaseProof(privateKey ed25519.PrivateKey, request ReleaseRequest) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize || request.ProofVersion != ReleaseProofVersion {
		return "", ErrInvalidReleaseProof
	}
	payload, err := canonicalReleaseProofPayload(request)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)), nil
}

func VerifyReleaseProof(request ReleaseRequest, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize ||
		request.ProofVersion != ReleaseProofVersion ||
		strings.TrimSpace(request.ProofSignature) == "" ||
		!keyBackedInstallationMatches(request.InstallationID, publicKey) {
		return ErrInvalidReleaseProof
	}
	signature, err := base64.StdEncoding.DecodeString(request.ProofSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidReleaseProof
	}
	payload, err := canonicalReleaseProofPayload(request)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return ErrInvalidReleaseProof
	}
	return nil
}

type renewalProofPayload struct {
	ProofVersion   int    `json:"proof_version"`
	LicenseID      string `json:"license_id"`
	InstallationID string `json:"installation_id"`
	CurrentNonce   string `json:"current_nonce"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	AppVersion     string `json:"app_version,omitempty"`
	BuildChannel   string `json:"build_channel"`
	BuildID        string `json:"build_id,omitempty"`
}

func canonicalRenewalProofPayload(request RenewalRequest) ([]byte, error) {
	payload := renewalProofPayload{
		ProofVersion:   request.ProofVersion,
		LicenseID:      strings.TrimSpace(request.LicenseID),
		InstallationID: strings.TrimSpace(request.InstallationID),
		CurrentNonce:   strings.TrimSpace(request.CurrentNonce),
		Platform:       strings.TrimSpace(request.Platform),
		Arch:           strings.TrimSpace(request.Arch),
		AppVersion:     strings.TrimSpace(request.AppVersion),
		BuildChannel:   strings.TrimSpace(request.BuildChannel),
		BuildID:        strings.TrimSpace(request.BuildID),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("entitlements.canonicalRenewalProofPayload: %w", err)
	}
	return raw, nil
}

func signRenewalProof(privateKey ed25519.PrivateKey, request RenewalRequest) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize || request.ProofVersion != RenewalProofVersion {
		return "", ErrInvalidRenewalProof
	}
	payload, err := canonicalRenewalProofPayload(request)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)), nil
}

// VerifyRenewalProof is public so a Go control-plane implementation can share
// the exact canonical proof contract. Production servers must still atomically
// consume CurrentNonce; a valid signature alone does not prevent replay.
func VerifyRenewalProof(request RenewalRequest, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize ||
		request.ProofVersion != RenewalProofVersion ||
		strings.TrimSpace(request.ProofSignature) == "" ||
		!keyBackedInstallationMatches(request.InstallationID, publicKey) {
		return ErrInvalidRenewalProof
	}
	signature, err := base64.StdEncoding.DecodeString(request.ProofSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidRenewalProof
	}
	payload, err := canonicalRenewalProofPayload(request)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return ErrInvalidRenewalProof
	}
	return nil
}
