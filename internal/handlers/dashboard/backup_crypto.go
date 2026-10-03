package dashboard

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	backupContentType       = "application/vnd.9router.backup"
	backupPassphraseHeader  = "x-9r-backup-passphrase"
	backupEncryptedMagic    = "9RBKENC1"
	backupEncryptedVersion  = byte(1)
	backupSaltSize          = 16
	backupNonceSize         = 12
	backupKeySize           = 32
	backupArgonTime         = uint32(3)
	backupArgonMemoryKiB    = uint32(64 * 1024)
	backupArgonParallelism  = uint8(1)
	backupMinPassphraseSize = 12
)

var (
	errBackupPassphraseRequired = errors.New("backup passphrase must be at least 12 characters")
	errBackupInvalid            = errors.New("invalid or corrupted encrypted backup")
	errBackupDecrypt            = errors.New("invalid backup passphrase or corrupted backup")
)

func validateBackupPassphrase(passphrase string) error {
	if len(passphrase) < backupMinPassphraseSize {
		return errBackupPassphraseRequired
	}
	return nil
}

func isEncryptedBackup(data []byte) bool {
	headerLen := len(backupEncryptedMagic) + 1
	return len(data) >= headerLen &&
		string(data[:len(backupEncryptedMagic)]) == backupEncryptedMagic &&
		data[len(backupEncryptedMagic)] == backupEncryptedVersion
}

// encryptBackup protects one serialized dashboard backup with a key derived
// from the caller-supplied passphrase. The fixed version-1 KDF parameters avoid
// attacker-controlled memory/time settings on restore.
func encryptBackup(plaintext []byte, passphrase string) ([]byte, error) {
	if err := validateBackupPassphrase(passphrase); err != nil {
		return nil, err
	}

	salt := make([]byte, backupSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate backup salt: %w", err)
	}
	nonce := make([]byte, backupNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate backup nonce: %w", err)
	}

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		backupArgonTime,
		backupArgonMemoryKiB,
		backupArgonParallelism,
		backupKeySize,
	)
	defer clear(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create backup cipher: %w", err)
	}
	gcm, err := cipherGCM(block)
	if err != nil {
		return nil, err
	}

	header := make([]byte, 0, len(backupEncryptedMagic)+1+backupSaltSize+backupNonceSize)
	header = append(header, []byte(backupEncryptedMagic)...)
	header = append(header, backupEncryptedVersion)
	header = append(header, salt...)
	header = append(header, nonce...)

	ciphertext := gcm.Seal(nil, nonce, plaintext, header)
	out := make([]byte, 0, len(header)+len(ciphertext))
	out = append(out, header...)
	out = append(out, ciphertext...)
	return out, nil
}

// decryptBackup authenticates the complete versioned header as AAD before
// returning plaintext. Wrong passphrases and tampering intentionally share one
// error so callers do not learn anything about encrypted contents.
func decryptBackup(data []byte, passphrase string) ([]byte, error) {
	if err := validateBackupPassphrase(passphrase); err != nil {
		return nil, err
	}
	headerLen := len(backupEncryptedMagic) + 1 + backupSaltSize + backupNonceSize
	if len(data) < headerLen+16 || !isEncryptedBackup(data) {
		return nil, errBackupInvalid
	}

	offset := len(backupEncryptedMagic) + 1
	salt := data[offset : offset+backupSaltSize]
	offset += backupSaltSize
	nonce := data[offset : offset+backupNonceSize]
	header := data[:headerLen]
	ciphertext := data[headerLen:]

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		backupArgonTime,
		backupArgonMemoryKiB,
		backupArgonParallelism,
		backupKeySize,
	)
	defer clear(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errBackupDecrypt
	}
	gcm, err := cipherGCM(block)
	if err != nil {
		return nil, errBackupDecrypt
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, header)
	if err != nil {
		return nil, errBackupDecrypt
	}
	return plaintext, nil
}

// cipherGCM is split out so all encrypted-backup code uses the same AEAD
// construction without exporting crypto details to the HTTP handler.
func cipherGCM(block cipher.Block) (cipher.AEAD, error) {
	return cipher.NewGCM(block)
}
