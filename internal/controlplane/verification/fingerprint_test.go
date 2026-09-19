package verification

import (
	"testing"
)

func TestFingerprint(t *testing.T) {
	tests := []struct {
		requested string
		actual    string
		expected  bool
	}{
		{"gpt-4", "gpt-4-0613", true},
		{"claude-3-sonnet", "claude-3-sonnet-20240229", true},
		{"gpt-4", "gpt-3.5-turbo", false},
		{"gpt-4", "", true}, // Be lenient if upstream doesn't report
	}

	for _, tt := range tests {
		res := Fingerprint(tt.requested, tt.actual, "")
		if res != tt.expected {
			t.Errorf("Fingerprint(%s, %s) expected %v got %v", tt.requested, tt.actual, tt.expected, res)
		}
	}
}
