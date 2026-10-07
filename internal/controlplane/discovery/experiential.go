package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/log"
)

// ExperientialModelsURL is the authenticated account-scoped model catalog.
// It is a variable so tests can point the adapter at an httptest server.
var ExperientialModelsURL = "https://api.experientiallabs.ai/api/v1/models"

type ExperientialFreeAdapter struct {
	db     *sql.DB
	client *http.Client
}

func NewExperientialFreeAdapter(db *sql.DB, client *http.Client) *ExperientialFreeAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ExperientialFreeAdapter{db: db, client: client}
}

func (a *ExperientialFreeAdapter) SourceID() string { return "experiential-free" }

func (a *ExperientialFreeAdapter) ScopedProviderIDs() []string {
	return []string{"experiential"}
}

type experientialCatalogModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CanonicalSlug string `json:"canonical_slug"`
}

type experientialConnectionData struct {
	APIKey      string `json:"apiKey"`
	AccessToken string `json:"accessToken"`
}

// Experiential's account-scoped model list includes native helpers that are
// callable by the organization but are not OpenAI chat models. Fabric's
// free-best route is a chat route, so those entries must never be advertised
// there. Jev is documented as /v1/systemone-only (no Chat Completions,
// Responses, or Messages facade).
func experientialFreeChatCompatible(model experientialCatalogModel) bool {
	id := strings.ToLower(strings.TrimSpace(model.ID))
	canonical := strings.ToLower(strings.TrimSpace(model.CanonicalSlug))
	if canonical == "jev-latest" || strings.HasPrefix(id, "type-safe/jev-") {
		return false
	}
	return true
}

// Discover publishes only provider-enforced :free aliases. Keeping the suffix
// in UpstreamModel guarantees that exhausting a promotion cannot fall through
// to paid account credit.
func (a *ExperientialFreeAdapter) Discover(ctx context.Context) ([]Candidate, error) {
	if a.db == nil {
		return nil, fmt.Errorf("Experiential discovery database unavailable")
	}

	rows, err := a.db.QueryContext(ctx, "SELECT id, data FROM providerConnections WHERE provider = ? AND isActive = 1", "experiential")
	if err != nil {
		return nil, fmt.Errorf("query Experiential connections: %w", err)
	}
	defer rows.Close()

	type credential struct {
		connID string
		key    string
	}
	var credentials []credential
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("scan Experiential connection: %w", err)
		}
		var data experientialConnectionData
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		key := strings.TrimSpace(data.APIKey)
		if key == "" {
			key = strings.TrimSpace(data.AccessToken)
		}
		if key != "" {
			credentials = append(credentials, credential{connID: id, key: key})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Experiential connections: %w", err)
	}

	if len(credentials) == 0 {
		return []Candidate{}, nil
	}

	now := time.Now().UTC()
	seen := make(map[string]struct{})
	var out []Candidate
	var transientErr error
	successfulCatalog := false

	for _, cred := range credentials {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ExperientialModelsURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create Experiential catalog request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+cred.key)

		resp, err := a.client.Do(req)
		if err != nil {
			transientErr = fmt.Errorf("Experiential catalog request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if readErr != nil {
			transientErr = fmt.Errorf("read Experiential catalog: %w", readErr)
			continue
		}

		switch resp.StatusCode {
		case http.StatusOK:
			successfulCatalog = true
		case http.StatusUnauthorized, http.StatusForbidden:
			continue
		default:
			transientErr = fmt.Errorf("Experiential catalog non-200 status: %d", resp.StatusCode)
			continue
		}

		var payload struct {
			Data   []experientialCatalogModel `json:"data"`
			Models []experientialCatalogModel `json:"models"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			transientErr = fmt.Errorf("decode Experiential catalog json: %w", err)
			continue
		}
		models := payload.Data
		if len(models) == 0 {
			models = payload.Models
		}

		for _, model := range models {
			id := strings.TrimSpace(model.ID)
			if id == "" || !strings.HasSuffix(strings.ToLower(id), ":free") || !experientialFreeChatCompatible(model) {
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}

			raw, _ := json.Marshal(model)
			out = append(out, Candidate{
				SourceID:      a.SourceID(),
				Provenance:    ExperientialModelsURL,
				Confidence:    1.0,
				RetrievalTime: now,
				ProviderID:    "experiential",
				ModelID:       id,
				UpstreamModel: id,
				PricingMode:   "free",
				Capabilities:  "{}",
				RawData:       string(raw),
			})
		}
	}

	if successfulCatalog {
		log.Info("discovery", "Experiential strict-free catalog sync complete", "candidates", len(out))
		return out, nil
	}
	if transientErr != nil {
		return nil, transientErr
	}

	log.Info("discovery", "Experiential strict-free catalog unavailable for active credentials", "candidates", 0)
	return []Candidate{}, nil
}
