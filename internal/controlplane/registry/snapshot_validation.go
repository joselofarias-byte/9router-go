package registry

import (
	"fmt"
)

// ValidateChecksum verifies that the expected checksum matches the actual SHA256 of the payload
func ValidateChecksum(expectedChecksum string, payload string) error {
	actual := GenerateChecksum(payload)
	if actual != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actual)
	}
	return nil
}
