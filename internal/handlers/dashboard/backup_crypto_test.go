package dashboard

import (
	"bytes"
	"testing"
)

func TestEncryptedBackupRoundTrip(t *testing.T) {
	plaintext := []byte(`{"providerConnections":[{"apiKey":"sk-secret","refreshToken":"refresh-secret"}]}`)
	encrypted, err := encryptBackup(plaintext, testBackupPassphrase)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !isEncryptedBackup(encrypted) {
		t.Fatal("encrypted payload missing 9rbak header")
	}
	if bytes.Contains(encrypted, []byte("sk-secret")) || bytes.Contains(encrypted, []byte("refresh-secret")) {
		t.Fatal("plaintext credential leaked into encrypted bytes")
	}

	decrypted, err := decryptBackup(encrypted, testBackupPassphrase)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	defer clear(decrypted)
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round trip mismatch: %q", decrypted)
	}
}

func TestEncryptedBackupRejectsWrongPassphraseAndTampering(t *testing.T) {
	encrypted, err := encryptBackup([]byte("sensitive"), testBackupPassphrase)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := decryptBackup(encrypted, "definitely-wrong-passphrase"); err == nil {
		t.Fatal("wrong passphrase decrypted backup")
	}

	tampered := append([]byte(nil), encrypted...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := decryptBackup(tampered, testBackupPassphrase); err == nil {
		t.Fatal("tampered backup decrypted successfully")
	}
}

func TestEncryptedBackupUsesFreshSaltAndNonce(t *testing.T) {
	plaintext := []byte("same plaintext")
	first, err := encryptBackup(plaintext, testBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptBackup(plaintext, testBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two backups of identical data produced identical ciphertext")
	}
}

func TestEncryptedBackupRequiresStrongEnoughPassphrase(t *testing.T) {
	if _, err := encryptBackup([]byte("data"), "short"); err == nil {
		t.Fatal("short passphrase was accepted")
	}
}
