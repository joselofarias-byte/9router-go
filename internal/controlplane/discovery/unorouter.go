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

var UnoRouterCatalogURL = "https://api.unorouter.com/v1/models"

type UnoRouterAdapter struct {
	client *http.Client
}

func NewUnoRouterAdapter(client *http.Client) *UnoRouterAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &UnoRouterAdapter{client: client}
}

func (a *UnoRouterAdapter) SourceID() string {
	return "unorouter"
}

func (a *UnoRouterAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UnoRouterCatalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create unorouter request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unorouter request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unorouter non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("read unorouter body: %w", err)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode unorouter json: %w", err)
	}

	var candidates []Candidate
	now := time.Now().UTC()

	for _, m := range payload.Data {
		// Preserve exact ID for routing
		pricingMode := "unknown"
		if strings.HasSuffix(m.ID, ":free") || strings.Contains(m.ID, "free") {
			pricingMode = "free"
		}

		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    "https://api.unorouter.com API",
			Confidence:    0.9,
			RetrievalTime: now,
			ProviderID:    "unorouter",
			ModelID:       m.ID,
			UpstreamModel: m.ID,
			PricingMode:   pricingMode,
			Capabilities:  "{}", // capabilities determined via probes later
		})
	}

	log.Info("discovery", "unorouter sync complete", "candidates", len(candidates))
	return candidates, nil
}
