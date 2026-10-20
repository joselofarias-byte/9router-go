package db

import (
    "bytes"
    "crypto/rand"
    "encoding/hex"
    "strings"
    "testing"
)

func TestGROK004DB02APIKeyLookupHMAC(t *testing.T) {
    master := make([]byte, 32)
    if _, err := rand.Read(master); err != nil { t.Fatal(err) }
    bearer := "sk-test-this-is-a-long-random-looking-credential"
    first, err := apiKeyLookupHMAC(master, bearer)
    if err != nil { t.Fatal(err) }
    again, err := apiKeyLookupHMAC(master, bearer)
    if err != nil { t.Fatal(err) }
    if first != again { t.Fatal("key lookup must be deterministic") }
    if len(first) != 64 || strings.Contains(first, bearer) {
        t.Fatal("key lookup index must be a fixed 256-bit hex HMAC")
    }
    if _, err := hex.DecodeString(first); err != nil { t.Fatal(err) }
    other, err := apiKeyLookupHMAC(master, bearer+"-different")
    if err != nil { t.Fatal(err) }
    if other == first { t.Fatal("distinct bearer keys share lookup digest") }
    otherMaster := bytes.Repeat([]byte{0x5a}, 32)
    other, err = apiKeyLookupHMAC(otherMaster, bearer)
    if err != nil { t.Fatal(err) }
    if other == first { t.Fatal("master-key rotation must change lookup index") }

    encryptionKey, err := deriveDBPurposeKey(master, dbEncryptionKeyContext)
    if err != nil { t.Fatal(err) }
    lookupKey, err := deriveDBPurposeKey(master, dbAPIKeyLookupContext)
    if err != nil { t.Fatal(err) }
    if bytes.Equal(lookupKey, encryptionKey) {
        t.Fatal("encryption and lookup keys must be separated")
    }
    if bytes.Equal(encryptionKey, master) {
        t.Fatal("raw master key must not be used directly for encryption")
    }
    if _, err := apiKeyLookupHMAC([]byte("weak"), bearer); err == nil {
        t.Fatal("weak master key accepted")
    }
    if _, err := apiKeyLookupHMAC(master, ""); err == nil {
        t.Fatal("empty bearer key accepted")
    }
    if _, err := deriveDBPurposeKey(master, "unexpected-context"); err == nil {
        t.Fatal("unknown key derivation context accepted")
    }
}
