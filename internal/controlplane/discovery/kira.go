package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/log"
)

// KiraAdapter discovers kiraai.vn's free-tier model lineup from its public
// catalog endpoint. Unlike OrcaRouter/UnoRouter, kiraai.vn's catalog
// (GET /v1/models/) is unauthenticated and explicitly flags each model's
// pricing tier via "is_free" — no API key is needed for discovery, only for
// actually dispatching a chat completion, so this adapter is safe to run
// unconditionally instead of being gated behind an opt-in env var.
type KiraAdapter struct {
	client  *http.Client
	baseURL string
}

// NewKiraAdapter builds a Kira discovery adapter. baseURL defaults to
// kiraai.vn's production API root when empty.
func NewKiraAdapter(client *http.Client, baseURL string) *KiraAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://kiraai.vn/v1"
	}
	return &KiraAdapter{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (a *KiraAdapter) SourceID() string {
	return "kira"
}

type kiraModel struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Type   string `json:"type"`
	Status string `json:"status"`
	IsFree bool   `json:"is_free"`
}

func (a *KiraAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	endpoint := a.baseURL + "/models/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("kira catalog request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kira catalog request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("kira authentication failed: %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("kira rate limited during discovery: %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kira catalog non-200 status: %d", resp.StatusCode)
	}

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("read kira catalog: %w", err)
	}

	var modelList struct {
		Data []kiraModel `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &modelList); err != nil {
		return nil, fmt.Errorf("decode kira catalog json: %w", err)
	}

	now := time.Now().UTC()
	var candidates []Candidate
	for _, m := range modelList.Data {
		if m.Object != "model" && m.Object != "" {
			continue
		}
		if m.Status != "" && m.Status != "active" {
			continue
		}
		// Only chat-capable free models are useful as fabric-free candidates —
		// Kira's catalog also lists paid models and non-chat modalities
		// (image/video/tts) that a chat pool must not surface.
		if !m.IsFree || (m.Type != "" && m.Type != "chat") {
			continue
		}

		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    endpoint,
			Confidence:    0.9,
			RetrievalTime: now,
			ProviderID:    "kira",
			ModelID:       m.ID,
			UpstreamModel: m.ID,
			PricingMode:   "free_tier",
			Capabilities:  "{}",
		})
	}

	log.Info("discovery", "kira sync complete", "candidates", len(candidates))
	return candidates, nil
}
