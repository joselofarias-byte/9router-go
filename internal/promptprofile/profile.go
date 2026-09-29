package promptprofile

import (
	"context"
	_ "embed"
	json "encoding/json/v2"
	"strings"
)

const (
	// Header selects a built-in prompt profile for the current request.
	Header = "X-9Router-Prompt-Profile"

	// OpenAIAgenticV1 is the first OpenAI/ChatGPT-oriented engineering profile.
	OpenAIAgenticV1 = "openai-agentic-v1"
)

//go:embed profiles/openai-agentic-v1.txt
var openAIAgenticV1Instructions string

// Profile is a fixed, named instruction profile. Callers select only the name;
// arbitrary header text is never promoted into a developer/system instruction.
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
	return []string{"none", OpenAIAgenticV1}
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
		return ctx
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

// Inject adds prompt as a developer-level instruction for Chat Completions,
// or as top-level instructions for a Responses API request. Existing caller
// instructions are preserved and extended; duplicate injection is a no-op.
func Inject(body []byte, prompt string) ([]byte, bool, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return body, false, nil
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body, false, err
	}

	// Responses API: prefer its native top-level instructions field whenever
	// input[] is present (or instructions already exists).
	_, hasInput := req["input"]
	_, hasInstructions := req["instructions"]
	if hasInput || hasInstructions {
		existing, _ := req["instructions"].(string)
		next := appendUnique(existing, prompt)
		if next == existing {
			return body, false, nil
		}
		req["instructions"] = next
		out, err := json.Marshal(req)
		return out, err == nil, err
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body, false, nil
	}

	clone := make([]any, len(messages))
	copy(clone, messages)

	// Extend an existing developer message when it has simple string content.
	for i, raw := range clone {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "developer" {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok {
			continue
		}
		next := appendUnique(content, prompt)
		if next == content {
			return body, false, nil
		}
		msg["content"] = next
		clone[i] = msg
		req["messages"] = clone
		out, err := json.Marshal(req)
		return out, err == nil, err
	}

	// No simple developer message: insert one after leading system/developer
	// messages and before the first user turn.
	insertAt := 0
	for insertAt < len(clone) {
		msg, ok := clone[insertAt].(map[string]any)
		if !ok {
			break
		}
		role, _ := msg["role"].(string)
		if role != "system" && role != "developer" {
			break
		}
		insertAt++
	}

	dev := map[string]any{"role": "developer", "content": prompt}
	next := make([]any, 0, len(clone)+1)
	next = append(next, clone[:insertAt]...)
	next = append(next, dev)
	next = append(next, clone[insertAt:]...)
	req["messages"] = next

	out, err := json.Marshal(req)
	return out, err == nil, err
}

// InjectCodex preserves the first system/developer instruction that the
// existing Codex adapter promotes into Responses API instructions, then
// appends the selected profile to that same instruction. This avoids losing
// either the caller's instruction or the profile during Chat-to-Responses
// conversion.
func InjectCodex(body []byte, prompt string) ([]byte, bool, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return body, false, nil
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body, false, err
	}

	_, hasInput := req["input"]
	_, hasInstructions := req["instructions"]
	if hasInput || hasInstructions {
		existing, _ := req["instructions"].(string)
		next := appendUnique(existing, prompt)
		if next == existing {
			return body, false, nil
		}
		req["instructions"] = next
		out, err := json.Marshal(req)
		return out, err == nil, err
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body, false, nil
	}

	clone := make([]any, len(messages))
	copy(clone, messages)
	for i, raw := range clone {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok {
			continue
		}
		next := appendUnique(content, prompt)
		if next == content {
			return body, false, nil
		}
		msg["content"] = next
		clone[i] = msg
		req["messages"] = clone
		out, err := json.Marshal(req)
		return out, err == nil, err
	}

	// No simple instruction-bearing message exists. A developer message becomes
	// Responses instructions in buildResponsesBody.
	dev := map[string]any{"role": "developer", "content": prompt}
	req["messages"] = append([]any{dev}, clone...)
	out, err := json.Marshal(req)
	return out, err == nil, err
}

func appendUnique(existing, prompt string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return prompt
	}
	if strings.Contains(existing, prompt) {
		return existing
	}
	return existing + "\n\n" + prompt
}
