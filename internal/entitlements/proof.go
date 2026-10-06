package entitlements

import (
	"crypto/ed25519"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

const RenewalProofVersion = 1

var ErrInvalidRenewalProof = errors.New("invalid renewal proof")

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
		strings.TrimSpace(request.ProofSignature) == "" {
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
