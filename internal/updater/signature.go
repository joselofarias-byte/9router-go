package updater

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
)

func verifyUpdateSignature(binary []byte, signature, publicKey string) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("configure trusted NINEROUTER_UPDATE_PUBLIC_KEY (base64 Ed25519)")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(key), binary, sig) {
		return errors.New("update Ed25519 signature rejected")
	}
	return nil
}
