package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const OpenRouterModelsURL = "https://openrouter.ai/api/v1/models"

// OpenRouterAdapter reads the official public catalog. Inference still needs
// a real OpenRouter connection. A ":free" suffix alone is not evidence of price.
type OpenRouterAdapter struct {
	client *http.Client
	url    string
}

func NewOpenRouterAdapter(client *http.Client) *OpenRouterAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenRouterAdapter{client: client, url: OpenRouterModelsURL}
}

func (a *OpenRouterAdapter) SourceID() string            { return "openrouter-official" }
func (a *OpenRouterAdapter) ScopedProviderIDs() []string { return []string{"openrouter"} }

func (a *OpenRouterAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenRouter catalog request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter catalog status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 10<<20 {
		return nil, fmt.Errorf("OpenRouter catalog exceeds 10 MiB")
	}
	payload, err := decodeOpenRouterCatalog(body)
	if err != nil {
		return nil, err
	}
	return openRouterCandidates(a.SourceID(), payload), nil
}

type openRouterModel struct {
	ID      string                     `json:"id"`
	Pricing map[string]json.RawMessage `json:"pricing"`
	Context int                        `json:"context_length"`
	Arch    struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	Parameters  []string `json:"supported_parameters"`
	TopProvider struct {
		MaxOutput int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

func decodeOpenRouterCatalog(body []byte) ([]openRouterModel, error) {
	var payload struct {
		Data *[]openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode OpenRouter catalog: %w", err)
	}
	if payload.Data == nil {
		return nil, fmt.Errorf("OpenRouter catalog missing data array")
	}
	return *payload.Data, nil
}

func openRouterCandidates(source string, models []openRouterModel) []Candidate {
	out := make([]Candidate, 0)
	seen := map[string]bool{}
	now := time.Now().UTC()
	for _, m := range models {
		if m.ID == "" || seen[m.ID] || !stringListContains(m.Arch.Output, "text") {
			continue
		}
		seen[m.ID] = true
		mode := "paid"
		if strings.HasSuffix(m.ID, ":free") && zeroOpenRouterPricing(m.Pricing) {
			mode = "free_tier"
		}
		caps, _ := json.Marshal(map[string]any{
			"chat": true, "tools": stringListContains(m.Parameters, "tools"),
			"reasoning":      stringListContains(m.Parameters, "reasoning"),
			"vision":         stringListContains(m.Arch.Input, "image"),
			"context_window": m.Context, "max_output": m.TopProvider.MaxOutput,
		})
		cost, _ := json.Marshal(m.Pricing)
		out = append(out, Candidate{
			SourceID: source, Provenance: OpenRouterModelsURL, Confidence: 1, RetrievalTime: now,
			ProviderID: "openrouter", ModelID: m.ID, UpstreamModel: m.ID,
			PricingMode: mode, Capabilities: string(caps), CostMetadata: string(cost),
		})
	}
	return out
}

func zeroOpenRouterPricing(pricing map[string]json.RawMessage) bool {
	if len(pricing["prompt"]) == 0 || len(pricing["completion"]) == 0 {
		return false
	}
	for key, raw := range pricing {
		if key == "overrides" && string(raw) == "[]" {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return false
		}
		n, ok := new(big.Rat).SetString(value)
		if !ok || n.Sign() != 0 {
			return false
		}
	}
	return true
}

func stringListContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
