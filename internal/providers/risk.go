package providers

// AccessMode describes the credential/surface boundary used to reach a
// provider. It is intentionally coarse: Fabric uses it for routing safety, not
// as an authentication implementation detail.
type AccessMode string

const (
	AccessAPIKey         AccessMode = "api_key"
	AccessOAuthAccount   AccessMode = "oauth_account"
	AccessProductSurface AccessMode = "product_surface"
	AccessNoAuth         AccessMode = "no_auth"
	AccessUnknown        AccessMode = "unknown"
)

type AccountRisk string

const (
	AccountRiskLow    AccountRisk = "low"
	AccountRiskMedium AccountRisk = "medium"
	AccountRiskHigh   AccountRisk = "high"
)

type ProbePolicy string

const (
	ProbeStandard     ProbePolicy = "standard"
	ProbeConservative ProbePolicy = "conservative"
)

// ProviderRiskProfile lets the control plane distinguish ordinary API-key
// traffic from account/subscription surfaces where aggressive automated use can
// put the user's upstream account at risk. ScorePenalty is deliberately modest:
// risky nodes remain usable as fallbacks, but equally capable low-risk nodes win.
type ProviderRiskProfile struct {
	AccessMode   AccessMode
	AccountRisk  AccountRisk
	ProbePolicy  ProbePolicy
	ScorePenalty float64
}

func GetProviderRiskProfile(provider string) ProviderRiskProfile {
	if cfg, ok := KnownProviders[provider]; ok && cfg.NoAuth {
		return ProviderRiskProfile{
			AccessMode:  AccessNoAuth,
			AccountRisk: AccountRiskLow,
			ProbePolicy: ProbeStandard,
		}
	}

	switch provider {
	case "antigravity":
		// Google-account backed and known to enforce anti-abuse controls. Keep it
		// routable, but prefer ordinary API-key/free endpoints when equivalent.
		return ProviderRiskProfile{
			AccessMode:   AccessOAuthAccount,
			AccountRisk:  AccountRiskHigh,
			ProbePolicy:  ProbeConservative,
			ScorePenalty: 15,
		}

	case "codex", "claude", "github", "kiro", "grok-cli", "grok-web", "gemini-cli",
		"qoder", "codebuddy-cn", "opencode-go", "clinepass", WorkBuddySessionProvider:
		return ProviderRiskProfile{
			AccessMode:   AccessProductSurface,
			AccountRisk:  AccountRiskMedium,
			ProbePolicy:  ProbeConservative,
			ScorePenalty: 7,
		}

	case "cline":
		// Cline exposes an official OpenAI-compatible API-key endpoint. OAuth may
		// also be used by clients, but the provider itself does not need to be
		// penalized like a personal-product surface.
		return ProviderRiskProfile{
			AccessMode:  AccessAPIKey,
			AccountRisk: AccountRiskLow,
			ProbePolicy: ProbeStandard,
		}

	case "codebuddy-intl":
		// WorkBuddy/CodeBuddy International officially documents personal API-key
		// authentication for individual developers (CODEBUDDY_API_KEY). Treat the
		// canonical international route as an ordinary quota-limited API-key
		// surface rather than browser/session automation. Quota exhaustion is
		// handled by normal health/quota telemetry, not by an account-risk penalty.
		return ProviderRiskProfile{
			AccessMode:  AccessAPIKey,
			AccountRisk: AccountRiskLow,
			ProbePolicy: ProbeStandard,
		}
	}

	if _, ok := KnownProviders[provider]; ok {
		return ProviderRiskProfile{
			AccessMode:  AccessAPIKey,
			AccountRisk: AccountRiskLow,
			ProbePolicy: ProbeStandard,
		}
	}

	return ProviderRiskProfile{
		AccessMode:  AccessUnknown,
		AccountRisk: AccountRiskMedium,
		ProbePolicy: ProbeConservative,
	}
}
