package updater

import "strings"

// Fork-safe update defaults prevent a Fabric-enabled build from silently
// replacing itself with an upstream-only binary. Operators can still override
// these values explicitly with UPDATE_URL and UPDATE_REPO.
const (
	forkUpdateManifestURL = "https://raw.githubusercontent.com/joselofarias-byte/9router-go/main/version.json"
	forkUpdateGitHubRepo   = "joselofarias-byte/9router-go"
	forkVersionSuffix      = "-fabric"
)

func init() {
	DefaultUpdateURL = forkUpdateManifestURL
	DefaultGitHubRepo = forkUpdateGitHubRepo

	// Keep the upstream numeric version as the update-comparison base while
	// making the running fork unmistakable in logs and the `version` command.
	// The inherited semver parser intentionally ignores '-' suffixes.
	if !strings.Contains(CurrentVersion, forkVersionSuffix) {
		CurrentVersion += forkVersionSuffix
	}
}
