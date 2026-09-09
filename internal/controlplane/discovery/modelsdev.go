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

var ModelsDevCatalogURL = "https://models.dev/api.json"

type ModelsDevAdapter struct {
	client *http.Client
}

func NewModelsDevAdapter(client *http.Client) *ModelsDevAdapter {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &ModelsDevAdapter{client: client}
}

func (a *ModelsDevAdapter) SourceID() string {
	return "models.dev"
}

func (a *ModelsDevAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelsDevCatalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create catalog request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog sync non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50MB limit
	if err != nil {
		return nil, fmt.Errorf("read catalog body: %w", err)
	}

	var rawData map[string]struct {
		Models map[string]struct {
			Modality map[string]bool `json:"modality"`
			Limit    *struct {
				Context int `json:"context"`
				Output  int `json:"output"`
			} `json:"limit"`
		} `json:"models"`
	}

	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, fmt.Errorf("decode catalog json: %w", err)
	}

	var candidates []Candidate
	now := time.Now().UTC()

	for provID, provData := range rawData {
		for modelID, mData := range provData.Models {
			base := strings.ToLower(modelID)
			if idx := strings.Index(base, "/"); idx != -1 {
				base = base[idx+1:]
			}
			if idx := strings.Index(base, ":"); idx != -1 {
				base = base[:idx]
			}

			caps := map[string]interface{}{
				"vision": mData.Modality["image"] || mData.Modality["vision"],
				"pdf":    mData.Modality["pdf"],
				"audio":  mData.Modality["audio"],
				"video":  mData.Modality["video"],
			}
			if mData.Limit != nil {
				caps["context_window"] = mData.Limit.Context
				caps["max_output"] = mData.Limit.Output
			}

			capsBytes, _ := json.Marshal(caps)

			candidates = append(candidates, Candidate{
				SourceID:      a.SourceID(),
				Provenance:    "https://models.dev API",
				Confidence:    0.8, // Good but still third-party aggregate
				RetrievalTime: now,
				ProviderID:    provID,
				ModelID:       base,
				UpstreamModel: modelID,
				PricingMode:   "unknown", // Models.dev doesn't provide granular pricing in this endpoint
				Capabilities:  string(capsBytes),
			})
		}
	}

	log.Info("discovery", "models.dev sync complete", "candidates", len(candidates))
	return candidates, nil
}
