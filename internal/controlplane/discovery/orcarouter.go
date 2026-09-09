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

type OrcaRouterAdapter struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func NewOrcaRouterAdapter(client *http.Client, baseURL, apiKey string) *OrcaRouterAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://orcarouter.ai/v1"
	}
	return &OrcaRouterAdapter{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
	}
}

func (a *OrcaRouterAdapter) SourceID() string {
	return "orcarouter"
}

func (a *OrcaRouterAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	// Fallback mechanism to robustly support generic OpenAI-compatible structures
	// if specific OrcaRouter endpoints fail.
	endpoints := []string{
		a.baseURL + "/public/models",
		a.baseURL + "/models/free",
		a.baseURL + "/models",
	}

	var rawBody []byte
	var successfulEndpoint string
	var err error

	for _, endpoint := range endpoints {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if e != nil {
			continue
		}

		if a.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+a.apiKey)
		}

		resp, e := a.client.Do(req)
		if e != nil {
			err = e
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			rawBody, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
			if err == nil {
				successfulEndpoint = endpoint
				break
			}
		} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("orcarouter authentication failed: %d", resp.StatusCode)
		} else if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("orcarouter rate limited during discovery: %d", resp.StatusCode)
		} else {
			err = fmt.Errorf("orcarouter sync non-200 status: %d", resp.StatusCode)
		}
	}

	if rawBody == nil {
		if err != nil {
			return nil, fmt.Errorf("orcarouter catalog request failed all endpoints: %w", err)
		}
		return nil, fmt.Errorf("orcarouter catalog request failed on all endpoints")
	}

	var modelList struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Mode string `json:"mode"`
			} `json:"pricing"` // Or similar structure if provided
			Object string `json:"object"`
			Cost   *struct {
				Prompt     float64 `json:"prompt"`
				Completion float64 `json:"completion"`
			} `json:"cost"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rawBody, &modelList); err != nil {
		return nil, fmt.Errorf("decode orcarouter catalog json: %w", err)
	}

	var candidates []Candidate
	now := time.Now().UTC()

	isFreeEndpoint := strings.Contains(successfulEndpoint, "/free") || strings.Contains(successfulEndpoint, "/public")

	for _, mData := range modelList.Data {
		if mData.Object != "model" && mData.Object != "" {
			continue
		}

		modelID := mData.ID
		pricingMode := "paid"

		// Infer free tier from endpoint heuristics or cost metrics if available
		if mData.Pricing.Mode != "" && mData.Pricing.Mode != "paid" {
			pricingMode = mData.Pricing.Mode
		} else if mData.Pricing.Mode == "paid" {
			pricingMode = "paid"
		} else if isFreeEndpoint {
			pricingMode = "free_tier"
		} else if mData.Cost != nil && mData.Cost.Prompt == 0 && mData.Cost.Completion == 0 {
			pricingMode = "free_tier"
		} else if strings.Contains(strings.ToLower(modelID), "free") || strings.Contains(strings.ToLower(modelID), "glm-5.3") {
			// Specific free-tier lineup detection, including GLM 5.3 Flash.
			pricingMode = "free_tier"
		}

		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    successfulEndpoint,
			Confidence:    0.9,
			RetrievalTime: now,
			ProviderID:    "orcarouter",
			ModelID:       modelID,
			UpstreamModel: modelID,
			PricingMode:   pricingMode,
			Capabilities:  `{}`,
		})
	}

	log.Info("discovery", "orcarouter sync complete", "candidates", len(candidates))
	return candidates, nil
}
