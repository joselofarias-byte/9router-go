package executor

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"9router/proxy/internal/translator"
)

// WorkBuddy Free accounts can use the official CodeBuddy CLI after an
// interactive browser login even when API-key creation is unavailable. Keep
// this surface separate from codebuddy-intl so API-key and product-session
// credentials never get conflated.
var workBuddySessionSemaphore = make(chan struct{}, 1)

const workBuddyLeanSystemPrompt = "You are a general-purpose assistant. Answer the user directly. Do not use tools."

type workBuddyCLIEvent struct {
	Type         string `json:"type"`
	Role         string `json:"role"`
	Status       string `json:"status"`
	Subtype      string `json:"subtype"`
	IsError      bool   `json:"is_error"`
	Result       string `json:"result"`
	SessionID    string `json:"session_id"`
	ProviderData struct {
		Model    string `json:"model"`
		RawUsage struct {
			PromptTokens           int     `json:"prompt_tokens"`
			CompletionTokens       int     `json:"completion_tokens"`
			TotalTokens            int     `json:"total_tokens"`
			PromptCacheWriteTokens int     `json:"prompt_cache_write_tokens"`
			CacheReadInputTokens   int     `json:"cache_read_input_tokens"`
			Credit                  float64 `json:"credit"`
		} `json:"rawUsage"`
	} `json:"providerData"`
}

type workBuddyCLIResult struct {
	Text                   string
	Model                  string
	SessionID              string
	PromptTokens           int
	CompletionTokens       int
	TotalTokens            int
	PromptCacheWriteTokens int
	CacheReadInputTokens   int
	Credit                 float64
}

// ForwardWorkBuddySession invokes the official CodeBuddy CLI using its saved
// browser-authenticated session. The first implementation is intentionally
// conservative: one request at a time, one turn, no tools, no session
// persistence, and a lean system prompt. This avoids treating a Free product
// session like an unlimited API credential.
func ForwardWorkBuddySession(w http.ResponseWriter, req *Request) error {
	select {
	case workBuddySessionSemaphore <- struct{}{}:
		defer func() { <-workBuddySessionSemaphore }()
	case <-req.Ctx.Done():
		return req.Ctx.Err()
	}

	model, prompt, err := workBuddyPromptFromBody(req.Body)
	if err != nil {
		return fmt.Errorf("workbuddy-session request: %w", err)
	}

	bin := strings.TrimSpace(os.Getenv("WORKBUDDY_CODEBUDDY_PATH"))
	if bin == "" {
		bin = "codebuddy"
	}

	args := []string{
		"-p",
		"--model", model,
		"--max-turns", "1",
		"--no-session-persistence",
		"--system-prompt", workBuddyLeanSystemPrompt,
		"--output-format", "json",
		prompt,
	}
	cmd := exec.CommandContext(req.Ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"CODEBUDDY_DISABLE_AUTO_MEMORY=1",
		"CODEBUDDY_CODE_DISABLE_AUTO_MEMORY=1",
		"CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS=1",
	)
	if cwd := strings.TrimSpace(os.Getenv("WORKBUDDY_SESSION_CWD")); cwd != "" {
		cmd.Dir = cwd
	} else if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
		cmd.Dir = home
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 2048 {
			msg = msg[len(msg)-2048:]
		}
		if msg == "" {
			msg = "CodeBuddy CLI failed; verify that the CLI is installed and logged in via the International Site"
		}
		return fmt.Errorf("workbuddy-session CLI: %w: %s", err, msg)
	}

	parsed, err := parseWorkBuddyCLIOutput(stdout.Bytes())
	if err != nil {
		return fmt.Errorf("workbuddy-session parse: %w", err)
	}
	if parsed.Model == "" {
		parsed.Model = model
	}
	if parsed.TotalTokens == 0 {
		parsed.TotalTokens = parsed.PromptTokens + parsed.CompletionTokens
	}

	usage := map[string]any{
		"prompt_tokens":               parsed.PromptTokens,
		"completion_tokens":           parsed.CompletionTokens,
		"total_tokens":                parsed.TotalTokens,
		"cache_creation_input_tokens": parsed.PromptCacheWriteTokens,
		"cache_read_input_tokens":     parsed.CacheReadInputTokens,
		"credit":                      parsed.Credit,
	}

	// Record the standard subset for 9router's normal usage accounting. The
	// custom credit field remains present in the client-facing OpenAI payload.
	translator.SetUsage(req.Ctx, &translator.OpenAIUsage{
		PromptTokens:             parsed.PromptTokens,
		CompletionTokens:         parsed.CompletionTokens,
		CacheCreationInputTokens: parsed.PromptCacheWriteTokens,
		CachedTokens:             parsed.CacheReadInputTokens,
	})

	responseID := fmt.Sprintf("chatcmpl-workbuddy-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	if req.IsStream {
		var sse bytes.Buffer
		first := map[string]any{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   parsed.Model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"role": "assistant", "content": parsed.Text},
			}},
		}
		finish := map[string]any{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   parsed.Model,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			}},
			"usage": usage,
		}
		for _, chunk := range []map[string]any{first, finish} {
			b, marshalErr := json.Marshal(chunk)
			if marshalErr != nil {
				return marshalErr
			}
			fmt.Fprintf(&sse, "data: %s\n\n", b)
		}
		sse.WriteString("data: [DONE]\n\n")
		return execSSEStream(w, bytes.NewReader(sse.Bytes()), req)
	}

	response := map[string]any{
		"id":      responseID,
		"object":  "chat.completion",
		"created": created,
		"model":   parsed.Model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": parsed.Text,
			},
			"finish_reason": "stop",
		}},
		"usage": usage,
	}
	b, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return jsonResponse(req.Ctx, w, bytes.NewReader(b), req.TranslateResp, req.ResponseBuf)
}

func workBuddyPromptFromBody(body []byte) (string, string, error) {
	var reqMap map[string]any
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return "", "", fmt.Errorf("invalid JSON: %w", err)
	}
	model, _ := reqMap["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return "", "", fmt.Errorf("missing model")
	}

	if tools, ok := reqMap["tools"].([]any); ok && len(tools) > 0 {
		return "", "", fmt.Errorf("tools are not supported by the WorkBuddy Free session adapter yet")
	}
	if toolChoice, ok := reqMap["tool_choice"]; ok && toolChoice != nil {
		if s, isString := toolChoice.(string); !isString || (s != "" && s != "none") {
			return "", "", fmt.Errorf("tool_choice is not supported by the WorkBuddy Free session adapter yet")
		}
	}

	messages, ok := reqMap["messages"].([]any)
	if !ok || len(messages) == 0 {
		return "", "", fmt.Errorf("missing messages")
	}

	var out strings.Builder
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		role = strings.TrimSpace(role)
		if role == "tool" {
			return "", "", fmt.Errorf("tool messages are not supported by the WorkBuddy Free session adapter yet")
		}
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			return "", "", fmt.Errorf("tool-call history is not supported by the WorkBuddy Free session adapter yet")
		}
		text, err := workBuddyContentText(msg["content"])
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if role == "" {
			role = "user"
		}
		fmt.Fprintf(&out, "[%s]\n%s\n\n", role, text)
	}
	prompt := strings.TrimSpace(out.String())
	if prompt == "" {
		return "", "", fmt.Errorf("messages contain no supported text")
	}
	return model, prompt, nil
}

func workBuddyContentText(content any) (string, error) {
	switch v := content.(type) {
	case string:
		return v, nil
	case []any:
		var parts []string
		for _, rawPart := range v {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			typeName, _ := part["type"].(string)
			switch typeName {
			case "text", "input_text", "output_text", "":
				if text, ok := part["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			case "image_url", "image", "input_image":
				return "", fmt.Errorf("image content is not supported by the WorkBuddy Free session adapter yet")
			case "tool_use", "tool_result", "tool_call":
				return "", fmt.Errorf("tool content is not supported by the WorkBuddy Free session adapter yet")
			}
		}
		return strings.Join(parts, "\n"), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("unsupported message content type")
	}
}

func parseWorkBuddyCLIOutput(raw []byte) (*workBuddyCLIResult, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty CodeBuddy output")
	}

	var events []workBuddyCLIEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		var single workBuddyCLIEvent
		if singleErr := json.Unmarshal(raw, &single); singleErr != nil {
			return nil, fmt.Errorf("unexpected CodeBuddy JSON: %w", err)
		}
		events = []workBuddyCLIEvent{single}
	}

	result := &workBuddyCLIResult{}
	foundSuccess := false
	for _, event := range events {
		if event.Role == "assistant" {
			if event.ProviderData.Model != "" {
				result.Model = event.ProviderData.Model
			}
			u := event.ProviderData.RawUsage
			if u.PromptTokens != 0 || u.CompletionTokens != 0 || u.Credit != 0 {
				result.PromptTokens = u.PromptTokens
				result.CompletionTokens = u.CompletionTokens
				result.TotalTokens = u.TotalTokens
				result.PromptCacheWriteTokens = u.PromptCacheWriteTokens
				result.CacheReadInputTokens = u.CacheReadInputTokens
				result.Credit = u.Credit
			}
		}
		if event.Type == "result" {
			if event.SessionID != "" {
				result.SessionID = event.SessionID
			}
			if event.IsError || event.Subtype == "error" {
				return nil, fmt.Errorf("CodeBuddy result error: %s", strings.TrimSpace(event.Result))
			}
			if event.Result != "" {
				result.Text = event.Result
				foundSuccess = true
			}
		}
	}
	if !foundSuccess {
		return nil, fmt.Errorf("CodeBuddy output contained no successful result")
	}
	return result, nil
}
