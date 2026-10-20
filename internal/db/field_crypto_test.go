package db

import (
    "bytes"
    "crypto/rand"
    "strings"
    "testing"
)

func TestGROK004DBFieldEncryptionRoundTripAndFailClosed(t *testing.T) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil { t.Fatal(err) }
    plaintext := []byte("sk-test-sensitive-provider-key-and-refresh-token")
    first, err := encryptDBField(key, "providerConnections", "account-01", "data", plaintext)
    if err != nil { t.Fatal(err) }
    second, err := encryptDBField(key, "providerConnections", "account-01", "data", plaintext)
    if err != nil { t.Fatal(err) }
    if first == second {
        t.Fatal("AES-GCM reused a nonce")
    }
    if strings.Contains(first, string(plaintext)) {
        t.Fatal("ciphertext contains plaintext")
    }
    recovered, err := decryptDBField(key, "providerConnections", "account-01", "data", first)
    if err != nil || !bytes.Equal(recovered, plaintext) {
        t.Fatalf("round trip failed: %v", err)
    }
    for _, context := range []struct{ table, row, col string }{
        {"apiKeys", "account-01", "data"},
        {"providerConnections", "account-02", "data"},
        {"providerConnections", "account-01", "apiKey"},
    } {
        if _, err := decryptDBField(key, context.table, context.row, context.col, first); err == nil {
            t.Fatal("accepted ciphertext moved to different DB context")
        }
    }
    wrongKey := bytes.Repeat([]byte{0}, 32)
    if _, err := decryptDBField(wrongKey, "providerConnections", "account-01", "data", first); err == nil {
        t.Fatal("accepted wrong decryption key")
    }
    if _, err := decryptDBField(key, "providerConnections", "account-01", "data", "plaintext-token"); err == nil {
        t.Fatal("accepted unencrypted legacy input without migration")
    }
    if _, err := encryptDBField([]byte("bad"), "providerConnections", "account-01", "data", plaintext); err == nil {
        t.Fatal("accepted weak key")
    }
    tampered := first[:len(first)-1] + "?"
    if _, err := decryptDBField(key, "providerConnections", "account-01", "data", tampered); err == nil {
        t.Fatal("accepted modified ciphertext")
    }
}
