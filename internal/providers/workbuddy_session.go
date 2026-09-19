package providers

// WorkBuddySessionProvider is the local, browser-authenticated CodeBuddy CLI
// surface. It is intentionally distinct from codebuddy-intl, which uses an
// API key and is available only to plans/accounts that can create one.
const WorkBuddySessionProvider = "workbuddy-session"

func init() {
	KnownProviders[WorkBuddySessionProvider] = ProviderConfig{
		// The executor does not dial BaseURL; it invokes the official CodeBuddy
		// CLI with the user's existing interactive login state. A non-empty
		// DefaultAPIKey gives the data plane a harmless virtual credential so a
		// DB connection is not required for this local session surface.
		BaseURL:       "local://codebuddy-session",
		DefaultAPIKey: "session",
	}

	// Keep the existing workbuddy/wb aliases pointing at codebuddy-intl for
	// backwards compatibility with API-key users. Free/session users opt in
	// explicitly with one of these aliases. The canonical provider name does
	// not need an alias entry; avoiding a self-alias preserves registry rules.
	ProviderAliasMap["wbs"] = WorkBuddySessionProvider
	ProviderAliasMap["wbf"] = WorkBuddySessionProvider
	ProviderAliasMap["workbuddy-free"] = WorkBuddySessionProvider
}
