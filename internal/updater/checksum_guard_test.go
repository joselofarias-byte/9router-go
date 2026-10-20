package updater

import (
    "crypto/sha256"
    "encoding/hex"
    "strings"
    "testing"
)

func TestGROK004ChecksumGuard(t *testing.T) {
    payload := []byte("known executable bytes")
    digest := sha256.Sum256(payload)
    correct := hex.EncodeToString(digest[:])
    cases := []struct {
        name string
        payload []byte
        hash string
        allowed bool
    }{
        {"correct", payload, correct, true},
        {"uppercase", payload, strings.ToUpper(correct), true},
        {"no digest", payload, "", false},
        {"malformed", payload, "xyz", false},
        {"wrong hash", payload, strings.Repeat("0", 64), false},
        {"tampered executable", []byte("different bytes"), correct, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if (verifyUpdateBinarySHA256(tc.payload, tc.hash) == nil) != tc.allowed {
                t.Fatalf("accepted mismatch: %s", tc.name)
            }
        })
    }
}
