package db

// GROK-004 / DB-02: building blocks for a future, explicitly approved migration.
// The index is not connected to the current SQLite schema or authentication path.
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "errors"
)

const (
    dbEncryptionKeyContext = "capimux:db02:aes-gcm:v1"
    dbAPIKeyLookupContext = "capimux:db02:api-key-lookup:v1"
    dbAPIKeyLookupValueContext = "capimux:db02:api-key:value:v1:"
)

// deriveDBPurposeKey isolates the encryption key from the lookup HMAC key.
// Requires a randomly generated 256-bit master key from an external key store.
// It is not intended to stretch human passwords; a passphrase needs a
// separately designed Argon2id derivation and associated recovery procedure.
func deriveDBPurposeKey(master []byte, purpose string) ([]byte, error) {
    if len(master) != 32 {
        return nil, errors.New("db: expected a 32-byte random master key")
    }
    switch purpose {
    case dbEncryptionKeyContext, dbAPIKeyLookupContext:
    default:
        return nil, errors.New("db: unknown key derivation purpose")
    }
    mac := hmac.New(sha256.New, master)
    _, _ = mac.Write([]byte(purpose))
    return mac.Sum(nil), nil
}

// apiKeyLookupHMAC returns a fixed-length keyed index suitable for an
// equality search on a new indexed column. Do not store unkeyed SHA-256 of
// bearer keys; even if bearer keys are usually random, an unkeyed hash can
// be attacked offline when legacy keys have poor entropy.
func apiKeyLookupHMAC(master []byte, bearerKey string) (string, error) {
    if bearerKey == "" {
        return "", errors.New("db: bearer key must not be empty")
    }
    indexKey, err := deriveDBPurposeKey(master, dbAPIKeyLookupContext)
    if err != nil {
        return "", err
    }
    mac := hmac.New(sha256.New, indexKey)
    _, _ = mac.Write([]byte(dbAPIKeyLookupValueContext))
    _, _ = mac.Write([]byte(bearerKey))
    return hex.EncodeToString(mac.Sum(nil)), nil
}
