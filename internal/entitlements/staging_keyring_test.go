package entitlements

import (
	"crypto/ed25519"
	"testing"
)

func TestStagingKeyRing(t *testing.T) {
	keys, err := StagingKeyRing()
	if err != nil {
		t.Fatalf("StagingKeyRing() error = %v", err)
	}
	key, ok := keys[StagingSigningKeyID]
	if !ok {
		t.Fatalf("missing staging key %q", StagingSigningKeyID)
	}
	if len(key) != ed25519.PublicKeySize {
		t.Fatalf("public key len = %d, want %d", len(key), ed25519.PublicKeySize)
	}
}
