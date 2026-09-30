package providers

import "strings"

// LocalCloudFallbackPolicy is a proposed, unwired switch for a future
// local-then-cloud route (llama-server down → a named cloud provider).
//
// It is intentionally not consulted by chat, media, combo, or executor code.
// Calling Decide does not open a connection or read the environment.
//
// The struct has no credential fields. A cloud hop, when it is eventually
// wired, must resolve CloudProvider through the existing connection store
// under a separate explicit opt-in. This type must not grow apiKey, token,
// secret, or proxy URL fields — those would mix a local route with secrets
// before the hop is reviewed.
type LocalCloudFallbackPolicy struct {
	// Enabled defaults to false. A zero policy never proposes a hop.
	Enabled bool
	// CloudProvider is a provider id such as "openai", never a URL or key.
	CloudProvider string
	// CloudModel is the upstream model id, without credentials.
	CloudModel string
	// On lists local failure classes that may propose a hop.
	// Allowed values: "unreachable", "timeout", "http_5xx".
	// Authentication failures and other 4xx responses are never eligible.
	On []string
}

// FallbackDecision is the result of an unwired policy check.
// Hop is true only as a proposal. Nothing in the request path acts on it.
type FallbackDecision struct {
	Hop           bool
	Reason        string
	CloudProvider string
	CloudModel    string
}

// Decide reports whether a future router could propose a cloud hop for kind.
// kind is a failure class, not a request body. The decision carries provider
// and model ids only.
func (p LocalCloudFallbackPolicy) Decide(kind string) FallbackDecision {
	if !p.Enabled || strings.TrimSpace(p.CloudProvider) == "" || strings.TrimSpace(p.CloudModel) == "" {
		return FallbackDecision{Hop: false, Reason: "disabled"}
	}
	switch kind {
	case "unreachable", "timeout", "http_5xx":
		for _, allowed := range p.On {
			if allowed == kind {
				return FallbackDecision{
					Hop:           true,
					Reason:        kind,
					CloudProvider: strings.TrimSpace(p.CloudProvider),
					CloudModel:    strings.TrimSpace(p.CloudModel),
				}
			}
		}
		return FallbackDecision{Hop: false, Reason: "trigger-not-listed"}
	default:
		return FallbackDecision{Hop: false, Reason: "not-eligible"}
	}
}
