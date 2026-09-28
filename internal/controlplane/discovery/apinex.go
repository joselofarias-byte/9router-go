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

const defaultAPInexBaseURL = "https://api.apinex.bond/v1"

// APInexAdapter discovers APInex models that the upstream catalog explicitly
// places in the free/ namespace. Dispatch remains authenticated with the
// user's APInex connection; discovery never stores credentials in Fabric.
type APInexAdapter struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func NewAPInexAdapter(client *http.Client, baseURL, apiKey string) *APInexAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPInexBaseURL
	}
	return &APInexAdapter{
		client:  client,
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
	}
}

func (a *APInexAdapter) SourceID() string { return "apinex" }

type apinexCatalogModel struct {
	ID     string `json:"id"`
	Object string `json:"object"`
}

func (a *APInexAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	endpoint := a.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create APInex catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("APInex catalog request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("APInex authentication failed: %d", resp.StatusCode)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("APInex rate limited during discovery: %d", resp.StatusCode)
	default:
		return nil, fmt.Errorf("APInex catalog non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read APInex catalog: %w", err)
	}

	var payload struct {
		Data []apinexCatalogModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode APInex catalog json: %w", err)
	}

	now := time.Now().UTC()
	seen := make(map[string]struct{}, len(payload.Data))
	candidates := make([]Candidate, 0, len(payload.Data))

	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" || (model.Object != "" && model.Object != "model") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(id), "free/") {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}

		raw, _ := json.Marshal(model)
		candidates = append(candidates, Candidate{
			SourceID:      a.SourceID(),
			Provenance:    endpoint,
			Confidence:    0.98,
			RetrievalTime: now,
			ProviderID:    "apinex",
			ModelID:       id,
			UpstreamModel: id,
			PricingMode:   "free",
			Capabilities:  "{}",
			RawData:       string(raw),
		})
	}

	log.Info("discovery", "APInex free catalog sync complete", "candidates", len(candidates))
	return candidates, nil
}
