// Package promptprofile selects a fixed, opt-in instruction profile for a
// single request. No profile is active unless the caller names one.
package promptprofile

import (
	"context"
	_ "embed"
	"strings"
)

const (
	// Header selects a built-in prompt profile for the current request.
	Header = "X-9Router-Prompt-Profile"

	// OpenAIAgenticV1 is the balanced OpenAI/ChatGPT-oriented engineering profile.
	OpenAIAgenticV1 = "openai-agentic-v1"
	// WorkspaceContextV1 emphasizes repository/workspace state as source of truth.
	WorkspaceContextV1 = "workspace-context-v1"
	// FewshotRoutingV1 provides compact examples for action/tool routing decisions.
	FewshotRoutingV1 = "fewshot-routing-v1"
	// OperatingSpecV1 applies an explicit acquire-act-verify-report operating loop.
	OperatingSpecV1 = "operating-spec-v1"
)

//go:embed profiles/openai-agentic-v1.txt
var openAIAgenticV1Instructions string

//go:embed profiles/workspace-context-v1.txt
var workspaceContextV1Instructions string

//go:embed profiles/fewshot-routing-v1.txt
var fewshotRoutingV1Instructions string

//go:embed profiles/operating-spec-v1.txt
var operatingSpecV1Instructions string

// Profile is a fixed, named instruction profile. Callers select only the name;
// arbitrary header text is never promoted into a developer or system instruction.
type Profile struct {
	Name         string
	Instructions string
	Providers    []string
}

var profiles = map[string]Profile{
	OpenAIAgenticV1: {
		Name:         OpenAIAgenticV1,
		Instructions: strings.TrimSpace(openAIAgenticV1Instructions),
		Providers:    []string{"openai", "codex"},
	},
	WorkspaceContextV1: {
		Name:         WorkspaceContextV1,
		Instructions: strings.TrimSpace(workspaceContextV1Instructions),
		Providers:    []string{"openai", "codex"},
	},
	FewshotRoutingV1: {
		Name:         FewshotRoutingV1,
		Instructions: strings.TrimSpace(fewshotRoutingV1Instructions),
		Providers:    []string{"openai", "codex"},
	},
	OperatingSpecV1: {
		Name:         OperatingSpecV1,
		Instructions: strings.TrimSpace(operatingSpecV1Instructions),
		Providers:    []string{"openai", "codex"},
	},
}

// Lookup returns a canonical built-in profile. "none" means explicit opt-out.
func Lookup(name string) (Profile, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "none" {
		return Profile{}, true
	}
	p, ok := profiles[name]
	return p, ok
}

// Names returns the accepted request-header values in stable order.
func Names() []string {
	return []string{
		"none",
		OpenAIAgenticV1,
		WorkspaceContextV1,
		FewshotRoutingV1,
		OperatingSpecV1,
	}
}

// Supports reports whether the profile is intended for the resolved provider.
func (p Profile) Supports(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, candidate := range p.Providers {
		if provider == candidate {
			return true
		}
	}
	return false
}

type contextKey struct{}

// WithName stores a canonical profile name in a request context.
func WithName(ctx context.Context, name string) context.Context {
	if ctx == nil {
		return nil
	}
	p, ok := Lookup(name)
	if !ok || p.Name == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, p.Name)
}

// FromContext returns the selected profile name, if any.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	name, _ := ctx.Value(contextKey{}).(string)
	return name
}
