package backup

import (
	"bytes"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"testing"
)

const testPassphrase = "correct horse battery staple"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte(`{"providerConnections":[{"apiKey":"sk-secret","refreshToken":"rt-secret"}]}`)
	blob, err := Encrypt(plaintext, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("sk-secret")) || bytes.Contains(blob, []byte("rt-secret")) {
		t.Fatal("encrypted backup leaked plaintext credentials")
	}
	if !IsEnvelope(blob) {
		t.Fatal("encrypted backup was not recognized as an envelope")
	}

	got, err := Decrypt(blob, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip mismatch: %s", got)
	}
}

func TestEncryptUsesFreshSaltAndNonce(t *testing.T) {
	plaintext := []byte(`{"same":"payload"}`)
	a, err := Encrypt(plaintext, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(plaintext, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encrypted backups of the same payload must differ")
	}
}

func TestDecryptRejectsWrongPassphrase(t *testing.T) {
	blob, err := Encrypt([]byte("secret"), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(blob, "this passphrase is wrong"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("wrong passphrase error = %v, want ErrDecryptFailed", err)
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	blob, err := Encrypt([]byte("secret"), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	var env Envelope
	if err := json.Unmarshal(blob, &env); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0x01
	env.Ciphertext = base64.RawStdEncoding.EncodeToString(raw)
	tampered, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(tampered, testPassphrase); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("tampered backup error = %v, want ErrDecryptFailed", err)
	}
}

func TestEncryptRequiresRecoveryPassphrase(t *testing.T) {
	if _, err := Encrypt([]byte("secret"), "too-short"); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("short passphrase error = %v", err)
	}
}
