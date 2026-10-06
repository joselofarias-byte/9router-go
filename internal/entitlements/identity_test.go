package entitlements

import (
	"crypto/ed25519"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRuntimeStoreInstallationIdentityIsKeyBackedAndStable(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())

	first, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("installation id changed: %q != %q", first.ID, second.ID)
	}
	if !strings.HasPrefix(first.ID, installationIDPrefix) {
		t.Fatalf("installation id %q is not key-backed", first.ID)
	}
	if first.ID != installationIDFromPublicKey(first.PublicKey) {
		t.Fatal("installation id is not the public-key fingerprint")
	}
	if len(first.PublicKey) != ed25519.PublicKeySize || len(first.PrivateKey) != ed25519.PrivateKeySize {
		t.Fatal("unexpected installation key sizes")
	}
	if string(first.PublicKey) != string(second.PublicKey) {
		t.Fatal("installation public key changed")
	}

	info, err := os.Stat(store.installationKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("installation key permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestRuntimeStoreInstallationIdentityDetectsKeyReplacement(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	identity, err := store.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}

	otherStore := NewRuntimeStore(t.TempDir())
	other, err := otherStore.InstallationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID == other.ID {
		t.Fatal("test identities unexpectedly match")
	}

	otherRaw, err := os.ReadFile(otherStore.installationKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.installationKeyPath(), otherRaw, privateFilePerm); err != nil {
		t.Fatal(err)
	}

	_, err = store.InstallationIdentity()
	if !errors.Is(err, ErrInstallationKeyMismatch) {
		t.Fatalf("error = %v, want %v", err, ErrInstallationKeyMismatch)
	}
}

func TestRuntimeStoreCorruptInstallationProofKeyFailsClosed(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	if _, err := store.InstallationIdentity(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.installationKeyPath(), []byte("not-base64\n"), privateFilePerm); err != nil {
		t.Fatal(err)
	}
	_, err := store.InstallationIdentity()
	if !errors.Is(err, ErrInvalidInstallationKey) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidInstallationKey)
	}
}
