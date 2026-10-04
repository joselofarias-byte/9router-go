package freecoding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// Chat endpoints match internal/providers known free-provider bases.
	openRouterChatURL = "https://openrouter.ai/api/v1/chat/completions"
	clineChatURL      = "https://api.cline.bot/api/v1/chat/completions"

	maxCompletionTokens = 1500
)

// InferencePolicy is the opt-in gate for a paid-balance-free call.
// Flag must be exactly "1" (FREE_CODING_INFERENCE). OPENROUTER_API_KEY is
// never read here; callers pass only a dedicated free credential.
type InferencePolicy struct {
	Flag          string
	Provider      string
	ModelID       string
	Endpoint      string
	APIKey        string
	Allow         AllowList
	AllowLoopback bool
}

// InferenceAllowed returns a skip reason, or an empty string when a single
// authenticated request may be sent.
func InferenceAllowed(p InferencePolicy) string {
	if p.Flag != "1" {
		return "FREE_CODING_INFERENCE is not 1; authenticated inference is opt-in and was not run"
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return "no free credential; set OPENROUTER_FREE_API_KEY or CLINE_FREE_API_KEY. Ambient OPENROUTER_API_KEY is ignored"
	}
	if isVirtualProfile(p.ModelID) {
		return "virtual profile names are not exact model ids; pick a discovered model id"
	}
	switch p.Provider {
	case "openrouter":
		if !strings.HasSuffix(p.ModelID, ":free") {
			return "refuse OpenRouter model without a :free suffix"
		}
	case "cline":
	default:
		return fmt.Sprintf("provider %q is outside this harness (openrouter and cline only)", p.Provider)
	}
	if !p.Allow.Has(p.Provider, p.ModelID) {
		return "model is not on the free-catalog allowlist; generate one with discover or offline and pass -allow"
	}
	if err := endpointAllowed(p.Provider, p.Endpoint, p.AllowLoopback); err != nil {
		return err.Error()
	}
	return ""
}

func isVirtualProfile(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "free", "free-best", "coding-best-free", "reasoning-free", "fast-free", "long-context", "local", "paid-fallback":
		return true
	default:
		return false
	}
}

func endpointAllowed(provider, endpoint string, loopback bool) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("refuse endpoint %q", endpoint)
	}
	if loopback && (u.Scheme == "http" || u.Scheme == "https") && isLoopback(u.Hostname()) {
		return nil
	}
	want := openRouterChatURL
	if provider == "cline" {
		want = clineChatURL
	} else if provider != "openrouter" {
		return fmt.Errorf("refuse endpoint for provider %q", provider)
	}
	w, err := url.Parse(want)
	if err != nil {
		return err
	}
	if u.Scheme != w.Scheme || !strings.EqualFold(u.Hostname(), w.Hostname()) || u.Path != w.Path || u.Port() != "" {
		return fmt.Errorf("refuse endpoint %q for %s", endpoint, provider)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DefaultEndpoint is the direct provider chat URL for a supported provider.
func DefaultEndpoint(provider string) (string, error) {
	switch provider {
	case "openrouter":
		return openRouterChatURL, nil
	case "cline":
		return clineChatURL, nil
	default:
		return "", fmt.Errorf("provider %q is outside this harness", provider)
	}
}

// InferInput is one authenticated attempt. AllowLoopback is for tests that
// point at httptest; the CLI does not set it.
type InferInput struct {
	Policy     InferencePolicy
	Task       string
	Run        int
	Now        time.Time
	HTTPClient *http.Client
}

// MakeSkip builds an inference_skip row. No request was sent.
func MakeSkip(provider, modelID, task, reason, provenance string, now time.Time) Record {
	if provenance == "" {
		provenance = "authenticated inference not run"
	}
	note := "no request sent"
	if task == "" {
		note = "tasks not started: bugfix, refactor, review; no request sent"
	}
	return Record{
		RecordType: RecordInferSkip,
		UTC:        now.UTC().Format(time.RFC3339),
		Tier:       TierInference,
		Provider:   provider,
		ModelID:    modelID,
		Task:       task,
		Skip:       true,
		SkipReason: reason,
		Provenance: provenance,
		QuotaScope: QuotaNotApplicable,
		Note:       note,
	}
}

// Infer sends at most one chat completion when the policy allows it, then
// grades the answer locally. The API key is never written into the record.
func Infer(ctx context.Context, in InferInput) (Record, error) {
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if reason := InferenceAllowed(in.Policy); reason != "" {
		return MakeSkip(in.Policy.Provider, in.Policy.ModelID, in.Task, reason, in.Policy.Endpoint, now), nil
	}
	if in.Run < 1 {
		return Record{}, fmt.Errorf("run must be >= 1")
	}
	prompt, err := Prompt(in.Task)
	if err != nil {
		return Record{}, err
	}
	start := time.Now()
	status, header, body, callErr := postChat(ctx, in.HTTPClient, in.Policy.Endpoint, in.Policy.APIKey, in.Policy.ModelID, prompt)
	rec := Record{
		RecordType:     RecordTaskRun,
		UTC:            now.Format(time.RFC3339),
		Tier:           TierInference,
		Provider:       in.Policy.Provider,
		ModelID:        in.Policy.ModelID,
		Task:           in.Task,
		Run:            in.Run,
		HTTPStatus:     status,
		LatencyMs:      time.Since(start).Milliseconds(),
		Status429Count: 0,
		Provenance:     in.Policy.Endpoint,
		QuotaScope:     quotaScope(header, in.Policy.APIKey),
	}
	if status == http.StatusTooManyRequests {
		rec.Status429Count = 1
	}
	if callErr != nil {
		rec.Error = redact(truncate(callErr.Error(), 400), in.Policy.APIKey)
		return rec, nil
	}
	if status != http.StatusOK {
		rec.Error = redact(truncate(apiError(body), 400), in.Policy.APIKey)
		return rec, nil
	}
	rec.ReportedCost = reportedCost(body)
	content := redact(assistantContent(body), in.Policy.APIKey)
	graded, err := Grade(in.Task, ExtractSubmission(in.Task, content))
	if err != nil {
		return Record{}, err
	}
	rec.Pass = graded.Pass
	rec.CompileOK = graded.CompileOK
	rec.TestOK = graded.TestOK
	rec.ReviewMatch = graded.ReviewMatch
	rec.Error = redact(graded.Error, in.Policy.APIKey)
	return rec, nil
}

func postChat(ctx context.Context, client *http.Client, endpoint, apiKey, model, prompt string) (int, http.Header, []byte, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{{
			"role":    "user",
			"content": prompt,
		}},
		"temperature": 0,
		"max_tokens":  maxCompletionTokens,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	clone := *client
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := clone.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

func quotaScope(h http.Header, secret string) string {
	if h == nil {
		return QuotaUnknown
	}
	var parts []string
	for _, key := range []string{"X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-Ratelimit-Reset"} {
		if v := h.Get(key); v != "" {
			parts = append(parts, key+"="+v)
		}
	}
	if len(parts) == 0 {
		return QuotaUnknown
	}
	return redact(strings.Join(parts, ";"), secret)
}

func reportedCost(body []byte) string {
	var payload struct {
		Usage struct {
			Cost      json.RawMessage `json:"cost"`
			TotalCost json.RawMessage `json:"total_cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	raw := payload.Usage.Cost
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = payload.Usage.TotalCost
	}
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func assistantContent(body []byte) string {
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Choices) == 0 {
		return ""
	}
	return payload.Choices[0].Message.Content
}

func apiError(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "non-200 response"
	}
	if payload.Error.Message == "" {
		return "non-200 response"
	}
	return payload.Error.Message
}

func redact(msg, secret string) string {
	if secret == "" || msg == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "[redacted]")
}
