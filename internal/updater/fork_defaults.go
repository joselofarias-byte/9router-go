package updater

// Fork-safe update defaults prevent a Fabric-enabled build from silently
// replacing itself with an upstream-only binary. Operators can still override
// these values explicitly with UPDATE_URL and UPDATE_REPO.
const (
	forkUpdateManifestURL = "https://raw.githubusercontent.com/joselofarias-byte/9router-go/main/version.json"
	forkUpdateGitHubRepo   = "joselofarias-byte/9router-go"
)

func init() {
	DefaultUpdateURL = forkUpdateManifestURL
	DefaultGitHubRepo = forkUpdateGitHubRepo
}
