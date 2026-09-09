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
	if !strings.HasSuffix(CurrentVersion, forkVersionSuffix) {
		t.Fatalf("fork build must expose Fabric identity, got %q", CurrentVersion)
	}
	if CompareVersions(CurrentVersion, strings.TrimSuffix(CurrentVersion, forkVersionSuffix)) != 0 {
		t.Fatalf("Fabric suffix must not change upstream numeric update ordering: %q", CurrentVersion)
	}
}
