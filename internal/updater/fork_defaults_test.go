package updater

import (
	"strings"
	"testing"
)

func TestForkUpdateDefaults(t *testing.T) {
	if DefaultGitHubRepo != forkUpdateGitHubRepo {
		t.Fatalf("unexpected default update repo: %q", DefaultGitHubRepo)
	}
	if DefaultUpdateURL != forkUpdateManifestURL {
		t.Fatalf("unexpected default update manifest: %q", DefaultUpdateURL)
	}
	if strings.Contains(DefaultUpdateURL, "luqman-v1/9router-go") || strings.Contains(DefaultGitHubRepo, "luqman-v1/9router-go") {
		t.Fatal("fork build must not default to upstream self-update sources")
	}
}
