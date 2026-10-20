package db

// GROK-004 DB-02: standalone authenticated encryption primitive.
// THIS CODE IS NOT YET WIRED TO ANY DATABASE WRITE PATH.
// Deployment requires a reviewed external key provider and a migration.
import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "strings"
)

const (
    encryptedDBFieldPrefix = "capimux:enc:v1:"
    encryptedDBFieldMaxSize = 16 << 20
)

// dbFieldAAD binds ciphertext to its exact database table, row, and field.
// Quoted JSON of a fixed-length string array is unambiguous and deterministic.
func dbFieldAAD(table, recordID, column string) ([]byte, error) {
    if table == "" || recordID == "" || column == "" {
        return nil, errors.New("db field: missing AAD context")
    }
    encoded, err := json.Marshal([3]string{table, recordID, column})
    if err != nil {
        return nil, err
    }
    return append([]byte("capimux-db-field-v1\x00"), encoded...), nil
}

// encryptDBField seals a sensitive value with a high-entropy external 256-bit
// master key. A fresh random AES-GCM nonce is generated for every encryption.
func encryptDBField(masterKey []byte, table, recordID, column string, plaintext []byte) (string, error) {
    if len(masterKey) != 32 {
        return "", errors.New("db field: master key must contain exactly 32 bytes")
    }
    if len(plaintext) > encryptedDBFieldMaxSize {
        return "", errors.New("db field: plaintext exceeds size limit")
    }
    aad, err := dbFieldAAD(table, recordID, column)
    if err != nil {
        return "", err
    }
    encryptionKey, err := deriveDBPurposeKey(masterKey, dbEncryptionKeyContext)
    if err != nil { return "", err }
    block, err := aes.NewCipher(encryptionKey)
    if err != nil {
        return "", err
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", fmt.Errorf("db field: nonce generation failed: %w", err)
    }
    blob := aead.Seal(nonce, nonce, plaintext, aad)
    return encryptedDBFieldPrefix + base64.RawStdEncoding.EncodeToString(blob), nil
}

// decryptDBField fails closed for plaintext, wrong keys, wrong row/column AAD,
// unknown envelope versions and modified ciphertext.
func decryptDBField(masterKey []byte, table, recordID, column, encoded string) ([]byte, error) {
    if len(masterKey) != 32 {
        return nil, errors.New("db field: invalid master key length")
    }
    if !strings.HasPrefix(encoded, encryptedDBFieldPrefix) {
        return nil, errors.New("db field: plaintext or unknown encryption format")
    }
    aad, err := dbFieldAAD(table, recordID, column)
    if err != nil {
        return nil, err
    }
    blob, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, encryptedDBFieldPrefix))
    if err != nil {
        return nil, errors.New("db field: invalid ciphertext encoding")
    }
    if len(blob) > encryptedDBFieldMaxSize+64 {
        return nil, errors.New("db field: ciphertext exceeds size limit")
    }
    encryptionKey, err := deriveDBPurposeKey(masterKey, dbEncryptionKeyContext)
    if err != nil { return nil, err }
    block, err := aes.NewCipher(encryptionKey)
    if err != nil {
        return nil, err
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    if len(blob) < aead.NonceSize()+aead.Overhead() {
        return nil, errors.New("db field: ciphertext too short")
    }
    nonce, ciphertext := blob[:aead.NonceSize()], blob[aead.NonceSize():]
    plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
    if err != nil {
        return nil, errors.New("db field: authentication failed")
    }
    return plaintext, nil
}
