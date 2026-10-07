package entitlements

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	installationKeyName   = "installation-key"
	installationIDPrefix  = "ik1_"
	installationSeedBytes = ed25519.SeedSize
)

var (
	ErrInvalidInstallationKey  = errors.New("invalid installation proof key")
	ErrInstallationKeyMismatch = errors.New("installation id does not match installation proof key")
)

// InstallationIdentity is the local proof-of-possession identity used by the
// licensing control plane. The private key is generated locally and must never
// be sent to the server. RuntimeStore currently protects the fallback key with
// private file permissions; platform-specific secure key stores can implement
// stronger non-exportability later without changing the wire proof format.
type InstallationIdentity struct {
	ID         string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func (s *RuntimeStore) installationKeyPath() string {
	return filepath.Join(s.dir, installationKeyName)
}

// InstallationIdentity returns the persisted installation id together with its
// local Ed25519 proof key. New installation ids are fingerprints of the public
// key. Legacy UUID installation ids remain readable for migration, but the
// caller should reactivate them so the control plane can bind the public key.
func (s *RuntimeStore) InstallationIdentity() (*InstallationIdentity, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, errors.New("entitlements.RuntimeStore: empty data directory")
	}

	id, err := s.InstallationID()
	if err != nil {
		return nil, err
	}
	privateKey, err := s.loadOrCreateInstallationPrivateKey()
	if err != nil {
		return nil, err
	}
	return bindInstallationIdentity(id, privateKey)
}

// persistedInstallationIdentity loads the installation proof key only when it
// is already stored. Renew and release use this so a copied installation id
// and cached lease cannot mint a replacement signer.
func (s *RuntimeStore) persistedInstallationIdentity() (*InstallationIdentity, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, errors.New("entitlements.RuntimeStore: empty data directory")
	}

	id, err := s.InstallationID()
	if err != nil {
		return nil, err
	}
	privateKey, err := s.loadInstallationPrivateKey()
	if err != nil {
		return nil, err
	}
	return bindInstallationIdentity(id, privateKey)
}

func bindInstallationIdentity(id string, privateKey ed25519.PrivateKey) (*InstallationIdentity, error) {
	publicKey := append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...)
	if strings.HasPrefix(id, installationIDPrefix) && id != installationIDFromPublicKey(publicKey) {
		return nil, ErrInstallationKeyMismatch
	}
	return &InstallationIdentity{
		ID:         id,
		PublicKey:  publicKey,
		PrivateKey: append(ed25519.PrivateKey(nil), privateKey...),
	}, nil
}

func (s *RuntimeStore) loadInstallationPrivateKey() (ed25519.PrivateKey, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, errors.New("entitlements.RuntimeStore: empty data directory")
	}

	path := s.installationKeyPath()
	var privateKey ed25519.PrivateKey
	err := withPathLock(path, func() error {
		raw, err := readRecoverable(path)
		if errors.Is(err, os.ErrNotExist) {
			return ErrInvalidInstallationKey
		}
		if err != nil {
			return err
		}
		privateKey, err = parseInstallationPrivateKey(raw)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("entitlements.RuntimeStore.loadInstallationPrivateKey: %w", err)
	}
	return privateKey, nil
}

func (s *RuntimeStore) loadOrCreateInstallationPrivateKey() (ed25519.PrivateKey, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, errors.New("entitlements.RuntimeStore: empty data directory")
	}

	path := s.installationKeyPath()
	var privateKey ed25519.PrivateKey
	err := withPathLock(path, func() error {
		raw, err := readRecoverable(path)
		if err == nil {
			privateKey, err = parseInstallationPrivateKey(raw)
			return err
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		_, generated, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("generate installation proof key: %w", err)
		}
		seed := generated.Seed()
		encoded := base64.StdEncoding.EncodeToString(seed)
		if err := atomicWriteFile(path, []byte(encoded+"\n")); err != nil {
			return fmt.Errorf("persist installation proof key: %w", err)
		}

		raw, err = readRecoverable(path)
		if err != nil {
			return fmt.Errorf("reload installation proof key: %w", err)
		}
		privateKey, err = parseInstallationPrivateKey(raw)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("entitlements.RuntimeStore.loadOrCreateInstallationPrivateKey: %w", err)
	}
	return privateKey, nil
}

func parseInstallationPrivateKey(raw []byte) (ed25519.PrivateKey, error) {
	encoded := strings.TrimSpace(string(raw))
	seed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(seed) != installationSeedBytes {
		return nil, ErrInvalidInstallationKey
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func installationIDFromPublicKey(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return installationIDPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

func parseKeyInstallationID(value string) (string, error) {
	if !strings.HasPrefix(value, installationIDPrefix) {
		return "", errors.New("not a key-backed installation id")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, installationIDPrefix))
	if err != nil || len(raw) != sha256.Size {
		return "", errors.New("invalid key-backed installation id")
	}
	return value, nil
}

func encodeInstallationPublicKey(publicKey ed25519.PublicKey) (string, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return "", ErrInvalidInstallationKey
	}
	return base64.StdEncoding.EncodeToString(publicKey), nil
}
