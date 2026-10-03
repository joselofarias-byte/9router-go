package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"9router/proxy/internal/log"
	"9router/proxy/internal/promptprofile"
)

func withRequestPromptProfile(ctx context.Context, r *http.Request) (context.Context, error) {
	if r == nil {
		return ctx, nil
	}
	name := strings.TrimSpace(r.Header.Get(promptprofile.Header))
	if name == "" {
		return ctx, nil
	}
	profile, ok := promptprofile.Lookup(name)
	if !ok {
		return ctx, fmt.Errorf("unknown prompt profile %q (supported: %s)", name, strings.Join(promptprofile.Names(), ", "))
	}
	if profile.Name == "" {
		return ctx, nil
	}
	return promptprofile.WithName(ctx, profile.Name), nil
}

// applySelectedPromptProfile injects the request's opt-in profile when the
// resolved provider is in that profile's scope. Other providers are unchanged.
func applySelectedPromptProfile(ctx context.Context, provider, model string, body []byte) ([]byte, error) {
	profileName := promptprofile.FromContext(ctx)
	if profileName == "" {
		return body, nil
	}
	profile, ok := promptprofile.Lookup(profileName)
	if !ok {
		return body, fmt.Errorf("unknown prompt profile %q", profileName)
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if !profile.Supports(provider) {
		return body, nil
	}
	inject := promptprofile.Inject
	if provider == "codex" {
		inject = promptprofile.InjectCodex
	}
	next, changed, err := inject(body, profile.Instructions)
	if err != nil {
		return body, fmt.Errorf("inject prompt profile %s: %w", profile.Name, err)
	}
	if !changed {
		return body, nil
	}
	log.Debug("prompt_profile", "injected", "profile", profile.Name, "provider", provider, "model", model)
	return next, nil
}
