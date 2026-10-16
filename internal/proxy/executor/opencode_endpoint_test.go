package executor

import "testing"

func TestOpencodeResponsesURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{"legacy zen", "https://opencode.ai/zen/v1/chat/completions", "https://opencode.ai/zen/v1/responses"},
		{"configured inference", "https://opencode.ai/inference/v1/chat/completions", "https://opencode.ai/inference/v1/responses"},
		{"explicit responses", "https://opencode.ai/inference/v1/responses", "https://opencode.ai/inference/v1/responses"},
		{"trailing slash", "https://opencode.ai/inference/v1/chat/completions/", "https://opencode.ai/inference/v1/responses"},
		{"local upstream", "http://127.0.0.1:8080/v1/chat/completions", "http://127.0.0.1:8080/v1/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := opencodeResponsesURL(tt.base); got != tt.want {
				t.Fatalf("opencodeResponsesURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}
