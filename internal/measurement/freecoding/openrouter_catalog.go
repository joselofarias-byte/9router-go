package freecoding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/controlplane/discovery"
)

// openRouterModelsURL is the public catalog. This main has no
// discovery.OpenRouterAdapter, so the harness keeps the same unauthenticated
// read and the same pricing rules that adapter applied.
const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

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

func discoverOpenRouterCatalog(ctx context.Context, client *http.Client, catalogURL string) ([]discovery.Candidate, error) {
	body, err := fetchOpenRouterCatalog(ctx, client, catalogURL)
	if err != nil {
		return nil, err
	}
	return classifyOpenRouterCatalog(body)
}

func fetchOpenRouterCatalog(ctx context.Context, client *http.Client, catalogURL string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterCatalogURL(catalogURL), nil)
	if err != nil {
		return nil, fmt.Errorf("openrouter catalog request: %w", err)
	}
	// A fresh request has no credentials. Drop them if a caller copied a header.
	req.Header.Del("Authorization")
	req.Header.Del("X-Api-Key")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter catalog request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter catalog status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("openrouter catalog body: %w", err)
	}
	if len(body) > 10<<20 {
		return nil, fmt.Errorf("openrouter catalog exceeds 10 MiB")
	}
	return body, nil
}

func openRouterCatalogURL(catalogURL string) string {
	if strings.TrimSpace(catalogURL) == "" {
		return openRouterModelsURL
	}
	return catalogURL
}

func classifyOpenRouterCatalog(body []byte) ([]discovery.Candidate, error) {
	var payload struct {
		Data *[]openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode openrouter catalog: %w", err)
	}
	if payload.Data == nil {
		return nil, fmt.Errorf("openrouter catalog missing data array")
	}
	out := make([]discovery.Candidate, 0)
	seen := map[string]bool{}
	now := time.Now().UTC()
	for _, model := range *payload.Data {
		cand, ok := openRouterCandidate(model, now)
		if !ok || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		out = append(out, cand)
	}
	return out, nil
}

func openRouterCandidate(model openRouterModel, now time.Time) (discovery.Candidate, bool) {
	if model.ID == "" || !stringSliceHas(model.Arch.Output, "text") {
		return discovery.Candidate{}, false
	}
	mode := "paid"
	if strings.HasSuffix(model.ID, ":free") && zeroOpenRouterPricing(model.Pricing) {
		mode = "free_tier"
	}
	caps, _ := json.Marshal(map[string]any{
		"chat": true, "tools": stringSliceHas(model.Parameters, "tools"),
		"reasoning":      stringSliceHas(model.Parameters, "reasoning"),
		"vision":         stringSliceHas(model.Arch.Input, "image"),
		"context_window": model.Context, "max_output": model.TopProvider.MaxOutput,
	})
	cost, _ := json.Marshal(model.Pricing)
	return discovery.Candidate{
		SourceID: "openrouter-official", Provenance: openRouterModelsURL,
		Confidence: 1, RetrievalTime: now, ProviderID: "openrouter", ModelID: model.ID,
		UpstreamModel: model.ID, PricingMode: mode, Capabilities: string(caps), CostMetadata: string(cost),
	}, true
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

func stringSliceHas(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
