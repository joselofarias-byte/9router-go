package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultLlamaCppRootURL = "http://127.0.0.1:8080"

type LlamaCppAdapter struct {
	client  *http.Client
	rootURL string
}

func NewLlamaCppAdapter(client *http.Client, baseURL string) *LlamaCppAdapter {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &LlamaCppAdapter{client: client, rootURL: normalizeLlamaCppRootURL(baseURL)}
}

func (a *LlamaCppAdapter) SourceID() string { return "llamacpp-local" }

func normalizeLlamaCppRootURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return defaultLlamaCppRootURL
	}
	for _, suffix := range []string{"/v1/chat/completions", "/chat/completions", "/v1/models", "/models", "/v1"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	if base == "" {
		return defaultLlamaCppRootURL
	}
	return strings.TrimRight(base, "/")
}

func (a *LlamaCppAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.rootURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create llama.cpp model request: %w", err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama.cpp model request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llama.cpp model request status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read llama.cpp models: %w", err)
	}

	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode llama.cpp models: %w", err)
	}
	if payload.Object != "list" {
		return nil, fmt.Errorf("unexpected llama.cpp model payload")
	}

	now := time.Now().UTC()
	seen := make(map[string]bool, len(payload.Data))
	out := make([]Candidate, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    "local llama.cpp /v1/models",
			Confidence:    1,
			RetrievalTime: now,
			ProviderID:    "llamacpp",
			ModelID:       id,
			UpstreamModel: id,
			PricingMode:   "free",
			Capabilities:  `{"chat":true,"local":true}`,
		})
	}
	return out, nil
}
