package verification

import (
	"strings"
)

// Fingerprint evaluates an upstream model response to confirm identity.
func Fingerprint(requestedModel string, actualResponseModel string, rawResponseText string) bool {
	// Basic heuristic: check if actual response model matches the intent.
	// Many proxies silently substitute GPT-4 with 3.5.
	reqLower := strings.ToLower(requestedModel)
	actLower := strings.ToLower(actualResponseModel)

	if actLower != "" && !strings.Contains(actLower, reqLower) && !strings.Contains(reqLower, actLower) {
		// e.g. requested "gpt-4", got "gpt-3.5-turbo" -> fingerprint mismatch
		return false
	}

	return true
}
