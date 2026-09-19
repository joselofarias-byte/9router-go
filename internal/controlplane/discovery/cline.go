package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"9router/proxy/internal/log"
)

// ClineRecommendedModelsURL is public and returns Cline's live recommended,
// free, ClinePass, and cloud model sets. It is a variable so tests can point
// the adapter at an httptest server without touching production traffic.
var ClineRecommendedModelsURL = "https://api.cline.bot/api/v1/ai/cline/recommended-models"

type ClineFreeAdapter struct {
	client *http.Client
}

func NewClineFreeAdapter(client *http.Client) *ClineFreeAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ClineFreeAdapter{client: client}
}

func (a *ClineFreeAdapter) SourceID() string {
	return "cline-free"
}

type clineCatalogModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// Discover reads only the server-declared `free` set. Recommended and
// ClinePass models are intentionally ignored: a model being cheap/recommended
// is not evidence that it is free.
func (a *ClineFreeAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ClineRecommendedModelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Cline catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cline catalog request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cline catalog non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, fmt.Errorf("read Cline catalog body: %w", err)
	}

	var payload struct {
		Free []clineCatalogModel `json:"free"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Cline catalog json: %w", err)
	}

	now := time.Now().UTC()
	candidates := make([]Candidate, 0, len(payload.Free))
	seen := make(map[string]struct{}, len(payload.Free))
	for _, model := range payload.Free {
		if model.ID == "" {
			continue
		}
		if _, duplicate := seen[model.ID]; duplicate {
			continue
		}
		seen[model.ID] = struct{}{}

		raw, _ := json.Marshal(model)
		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    ClineRecommendedModelsURL,
			Confidence:    0.98,
			RetrievalTime: now,
			ProviderID:    "cline",
			ModelID:       model.ID,
			UpstreamModel: model.ID,
			PricingMode:   "free",
			Capabilities:  "{}", // verified separately by capability probes
			RawData:       string(raw),
		})
	}

	log.Info("discovery", "Cline free catalog sync complete", "candidates", len(candidates))
	return candidates, nil
}
