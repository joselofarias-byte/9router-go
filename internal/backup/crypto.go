package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	Format  = "9router-encrypted-backup"
	Version = 1

	cipherName = "AES-256-GCM"
	kdfName    = "Argon2id"

	defaultArgonTime    uint32 = 3
	defaultArgonMemory  uint32 = 64 * 1024
	defaultArgonThreads uint8  = 2
	keySize                    = 32
	saltSize                   = 16
)

var (
	ErrPassphraseRequired = errors.New("backup passphrase must be at least 12 characters")
	ErrInvalidBackup      = errors.New("invalid or unsupported encrypted backup")
	ErrDecryptFailed      = errors.New("invalid backup passphrase or corrupted backup")
)

type Envelope struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	Cipher     string    `json:"cipher"`
	KDF        string    `json:"kdf"`
	Parameters KDFParams `json:"parameters"`
	Nonce      string    `json:"nonce"`
	Ciphertext string    `json:"ciphertext"`
}

type KDFParams struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memoryKiB"`
	Threads   uint8  `json:"threads"`
	Salt      string `json:"salt"`
}

func Encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	if len(passphrase) < 12 {
		return nil, ErrPassphraseRequired
	}

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate backup salt: %w", err)
	}

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		defaultArgonTime,
		defaultArgonMemory,
		defaultArgonThreads,
		keySize,
	)
	defer wipe(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create backup cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create backup gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate backup nonce: %w", err)
	}

	env := Envelope{
		Format:  Format,
		Version: Version,
		Cipher:  cipherName,
		KDF:     kdfName,
		Parameters: KDFParams{
			Time:      defaultArgonTime,
			MemoryKiB: defaultArgonMemory,
			Threads:   defaultArgonThreads,
			Salt:      base64.RawStdEncoding.EncodeToString(salt),
		},
		Nonce: base64.RawStdEncoding.EncodeToString(nonce),
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, envelopeAAD(env))
	env.Ciphertext = base64.RawStdEncoding.EncodeToString(ciphertext)

	return json.Marshal(env)
}

func Decrypt(blob []byte, passphrase string) ([]byte, error) {
	if len(passphrase) < 12 {
		return nil, ErrPassphraseRequired
	}

	var env Envelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, ErrInvalidBackup
	}
	if err := validateEnvelope(env); err != nil {
		return nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(env.Parameters.Salt)
	if err != nil {
		return nil, ErrInvalidBackup
	}
	nonce, err := base64.RawStdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, ErrInvalidBackup
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, ErrInvalidBackup
	}

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		env.Parameters.Time,
		env.Parameters.MemoryKiB,
		env.Parameters.Threads,
		keySize,
	)
	defer wipe(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidBackup
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != gcm.NonceSize() || len(ciphertext) < gcm.Overhead() {
		return nil, ErrInvalidBackup
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, envelopeAAD(env))
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

func IsEnvelope(blob []byte) bool {
	var marker struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(blob, &marker); err != nil {
		return false
	}
	return marker.Format == Format
}

func validateEnvelope(env Envelope) error {
	if env.Format != Format || env.Version != Version || env.Cipher != cipherName || env.KDF != kdfName {
		return ErrInvalidBackup
	}
	if env.Parameters.Time < 1 || env.Parameters.Time > 8 {
		return ErrInvalidBackup
	}
	if env.Parameters.MemoryKiB < 16*1024 || env.Parameters.MemoryKiB > 256*1024 {
		return ErrInvalidBackup
	}
	if env.Parameters.Threads < 1 || env.Parameters.Threads > 8 {
		return ErrInvalidBackup
	}
	salt, err := base64.RawStdEncoding.DecodeString(env.Parameters.Salt)
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return ErrInvalidBackup
	}
	if env.Nonce == "" || env.Ciphertext == "" {
		return ErrInvalidBackup
	}
	return nil
}

func envelopeAAD(env Envelope) []byte {
	return []byte(fmt.Sprintf(
		"%s|%d|%s|%s|%d|%d|%d|%s|%s",
		env.Format,
		env.Version,
		env.Cipher,
		env.KDF,
		env.Parameters.Time,
		env.Parameters.MemoryKiB,
		env.Parameters.Threads,
		env.Parameters.Salt,
		env.Nonce,
	))
}

func wipe(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}
