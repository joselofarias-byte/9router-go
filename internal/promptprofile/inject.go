package promptprofile

import (
	json "encoding/json/v2"
	"strings"
)

// Inject adds prompt as a developer-level instruction for Chat Completions,
// or as top-level instructions for a Responses API request. Existing caller
// instructions are preserved and extended; duplicate injection is a no-op.
func Inject(body []byte, prompt string) ([]byte, bool, error) {
	req, prompt, ok, err := prepareInject(body, prompt)
	if err != nil || !ok {
		return body, false, err
	}
	if responsesBody(req) {
		return writeInstructions(body, req, prompt)
	}
	return injectChatDeveloper(body, req, prompt)
}

// InjectCodex preserves the first system or developer instruction that the
// Codex adapter promotes into Responses API instructions, then appends the
// selected profile to that same instruction. Existing caller instructions stay,
// and a repeated injection is a no-op.
func InjectCodex(body []byte, prompt string) ([]byte, bool, error) {
	req, prompt, ok, err := prepareInject(body, prompt)
	if err != nil || !ok {
		return body, false, err
	}
	if responsesBody(req) {
		return writeInstructions(body, req, prompt)
	}
	return injectCodexChat(body, req, prompt)
}

func prepareInject(body []byte, prompt string) (map[string]any, string, bool, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, "", false, nil
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "", false, err
	}
	return req, prompt, true, nil
}

func responsesBody(req map[string]any) bool {
	_, hasInput := req["input"]
	_, hasInstructions := req["instructions"]
	return hasInput || hasInstructions
}

func writeInstructions(body []byte, req map[string]any, prompt string) ([]byte, bool, error) {
	existing, _ := req["instructions"].(string)
	next := appendUnique(existing, prompt)
	if next == existing {
		return body, false, nil
	}
	req["instructions"] = next
	return marshalChanged(req)
}

func injectChatDeveloper(body []byte, req map[string]any, prompt string) ([]byte, bool, error) {
	messages, ok := req["messages"].([]any)
	if !ok {
		return body, false, nil
	}
	clone := append([]any(nil), messages...)
	found, changed := extendRole(clone, prompt, "developer")
	if found && !changed {
		return body, false, nil
	}
	if changed {
		req["messages"] = clone
		return marshalChanged(req)
	}

	insertAt := leadingInstructionCount(clone)
	dev := map[string]any{"role": "developer", "content": prompt}
	next := make([]any, 0, len(clone)+1)
	next = append(next, clone[:insertAt]...)
	next = append(next, dev)
	next = append(next, clone[insertAt:]...)
	req["messages"] = next
	return marshalChanged(req)
}

func injectCodexChat(body []byte, req map[string]any, prompt string) ([]byte, bool, error) {
	messages, ok := req["messages"].([]any)
	if !ok {
		return body, false, nil
	}
	clone := append([]any(nil), messages...)
	found, changed := extendRole(clone, prompt, "system", "developer")
	if found && !changed {
		return body, false, nil
	}
	if changed {
		req["messages"] = clone
		return marshalChanged(req)
	}

	dev := map[string]any{"role": "developer", "content": prompt}
	req["messages"] = append([]any{dev}, clone...)
	return marshalChanged(req)
}

// extendRole appends prompt to the first listed role whose content is a string.
// The bools report whether a matching message was found and whether it changed.
func extendRole(messages []any, prompt string, roles ...string) (found bool, changed bool) {
	for i, raw := range messages {
		msg, role, ok := messageRole(raw)
		if !ok || !roleListed(role, roles) {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok {
			continue
		}
		next := appendUnique(content, prompt)
		if next == content {
			return true, false
		}
		msg["content"] = next
		messages[i] = msg
		return true, true
	}
	return false, false
}

func leadingInstructionCount(messages []any) int {
	insertAt := 0
	for insertAt < len(messages) {
		_, role, ok := messageRole(messages[insertAt])
		if !ok || (role != "system" && role != "developer") {
			break
		}
		insertAt++
	}
	return insertAt
}

func messageRole(raw any) (map[string]any, string, bool) {
	msg, ok := raw.(map[string]any)
	if !ok {
		return nil, "", false
	}
	role, _ := msg["role"].(string)
	return msg, role, true
}

func roleListed(role string, roles []string) bool {
	for _, candidate := range roles {
		if role == candidate {
			return true
		}
	}
	return false
}

func marshalChanged(req map[string]any) ([]byte, bool, error) {
	out, err := json.Marshal(req)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
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
