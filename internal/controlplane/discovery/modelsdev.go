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
			Cost     *struct {
				Input  *float64 `json:"input"`
				Output *float64 `json:"output"`
			} `json:"cost"`
			Limit *struct {
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
			pricing := inferModelsDevPricing(mData.Cost)
			// Fabric free routing only keeps classified free models from this
			// third-party catalog. Paid/unknown entries stay out of the
			// snapshot so they cannot pollute fabric-free candidate selection.
			if pricing != "free" && pricing != "free_tier" {
				continue
			}

			candidates = append(candidates, Candidate{
				SourceID:      a.SourceID(),
				Provenance:    "https://models.dev API",
				Confidence:    0.8,
				RetrievalTime: now,
				ProviderID:    provID,
				ModelID:       base,
				UpstreamModel: modelID,
				PricingMode:   pricing,
				Capabilities:  string(capsBytes),
			})
		}
	}

	log.Info("discovery", "models.dev sync complete", "candidates", len(candidates))
	return candidates, nil
}

func inferModelsDevPricing(cost *struct {
	Input  *float64 `json:"input"`
	Output *float64 `json:"output"`
}) string {
	if cost == nil || (cost.Input == nil && cost.Output == nil) {
		return "unknown"
	}
	in := 0.0
	out := 0.0
	if cost.Input != nil {
		in = *cost.Input
	}
	if cost.Output != nil {
		out = *cost.Output
	}
	if in == 0 && out == 0 {
		return "free"
	}
	return "paid"
}
