package providers

import "testing"

func TestResolveAliasWorkBuddy(t *testing.T) {
	tests := map[string]string{
		"wb":        "codebuddy-intl",
		"workbuddy": "codebuddy-intl",
		"cbai":      "codebuddy-intl",
	}

	for input, want := range tests {
		if got := ResolveAlias(input); got != want {
			t.Fatalf("ResolveAlias(%q) = %q, want %q", input, got, want)
		}
	}
}
