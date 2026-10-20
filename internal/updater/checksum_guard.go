package updater

import (
    "crypto/sha256"
    "crypto/subtle"
    "encoding/hex"
    "fmt"
)

// validateUpdateSHA256 rejects absent or malformed digest metadata before I/O.
func validateUpdateSHA256(expected string) error {
    if len(expected) != sha256.Size*2 {
        return fmt.Errorf("updater: refusing update without a 64-character SHA-256 checksum")
    }
    if _, err := hex.DecodeString(expected); err != nil {
        return fmt.Errorf("updater: malformed SHA-256 checksum: %w", err)
    }
    return nil
}

// verifyUpdateBinarySHA256 verifies extracted executable bytes, not archive bytes.
func verifyUpdateBinarySHA256(executable []byte, expected string) error {
    if err := validateUpdateSHA256(expected); err != nil {
        return err
    }
    want, _ := hex.DecodeString(expected)
    actual := sha256.Sum256(executable)
    if subtle.ConstantTimeCompare(actual[:], want) != 1 {
        return fmt.Errorf("updater: SHA-256 checksum mismatch")
    }
    return nil
}
