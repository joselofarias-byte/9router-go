package providers

import "testing"

func TestDeepSeekV41FlashCapabilities(t *testing.T) {
	for _, tc := range []struct {
		provider string
		model    string
	}{
		{"deepseek", "deepseek-v4.1-flash"},
		{"deepseek", "deepseek-flash"},
		{"cline", "deepseek/deepseek-v4.1-flash"},
		{"codebuddy-cn", "deepseek-v4.1-flash"},
	} {
		caps := GetCapabilitiesForModel(tc.provider, tc.model)
		if !caps.Vision || !caps.Reasoning || !caps.Tools {
			t.Errorf("%s/%s should have Vision+Reasoning+Tools, got %+v", tc.provider, tc.model, caps)
		}
	}
}
