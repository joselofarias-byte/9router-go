package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
