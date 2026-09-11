package providers

import "testing"

// TestClassifyError_Taxonomy verifies ClassifyError distinguishes the outcome
// categories the Fabric live-probe pipeline relies on to tell "provider is
// unreachable" apart from "provider is up but this model/account is not" —
// conflating these previously left everything that wasn't an explicit 401/
// 403/404/429 lumped into the generic ErrTransient bucket.
func TestClassifyError_Taxonomy(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorText  string
		want       ErrorCategory
	}{
		{"auth 401", 401, "", ErrAuth},
		{"auth 403", 403, "", ErrAuth},
		{"rate limit 429", 429, "", ErrRateLimit},
		{"rate limit text", 0, "Rate limit exceeded, please retry", ErrRateLimit},
		{"quota 402", 402, "", ErrQuota},
		{"quota text", 0, "quota exceeded for this billing period", ErrQuota},
		{"model not found text", 404, "model not found: glm-9000", ErrModelNotFound},
		{"unknown model text", 400, "unknown model requested", ErrModelNotFound},
		{"does not exist text", 404, "the requested model does not exist", ErrModelNotFound},
		{"generic 404 without model text", 404, "", ErrPermanent},
		{"dns failure", 0, `Get "https://kiraai.vn/v1/chat/completions": dial tcp: lookup kiraai.vn: no such host`, ErrNetwork},
		{"connection refused", 0, "dial tcp 127.0.0.1:1: connect: connection refused", ErrNetwork},
		{"network unreachable", 0, "dial tcp: connect: network is unreachable", ErrNetwork},
		{"connection reset", 0, "read tcp: connection reset by peer", ErrNetwork},
		{"context deadline exceeded", 0, "context deadline exceeded", ErrTimeout},
		{"client timeout", 0, `Get "https://kiraai.vn/v1/models": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`, ErrTimeout},
		{"io timeout", 0, "read tcp: i/o timeout", ErrTimeout},
		{"unmatched transient", 502, "unexpected upstream failure", ErrTransient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.statusCode, tt.errorText, 0)
			if got.Category != tt.want {
				t.Errorf("ClassifyError(%d, %q) category = %q, want %q", tt.statusCode, tt.errorText, got.Category, tt.want)
			}
		})
	}
}
