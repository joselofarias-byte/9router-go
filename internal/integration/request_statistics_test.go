//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// Assert the dashboard sees the same measured usage as the production router.
func TestDashboardRequestStatistics(t *testing.T) {
	env, _ := newProviderEnv(t)
	res := env.Post(t, "/v1/chat/completions", ChatBody("deepseek/deepseek-chat", false))
	if res.Status != http.StatusOK {
		t.Fatalf("completion status %d: %s", res.Status, truncate(res.Body))
	}
	res = env.Get(t, "/api/usage/request-details?limit=20&offset=0")
	if res.Status != http.StatusOK {
		t.Fatalf("statistics status %d: %s", res.Status, truncate(res.Body))
	}
	var result struct {
		Total   int `json:"total"`
		Details []struct {
			Cost          *float64 `json:"cost"`
			CostEstimated bool     `json:"costEstimated"`
			ConnectionID  string   `json:"connectionId"`
			Endpoint      string   `json:"endpoint"`
			Tokens        struct {
				Input  int `json:"prompt_tokens"`
				Output int `json:"completion_tokens"`
			} `json:"tokens"`
		} `json:"details"`
	}
	res.Decode(t, &result)
	if result.Total != 1 || len(result.Details) != 1 {
		t.Fatalf("expected one completion, got %+v", result)
	}
	detail := result.Details[0]
	if detail.Cost == nil || *detail.Cost <= 0 || !detail.CostEstimated {
		t.Errorf("missing estimated cost: %+v", detail)
	}
	if detail.ConnectionID != "conn-deepseek" || detail.Endpoint != "/v1/chat/completions" {
		t.Errorf("lost routing destination: %+v", detail)
	}
	if detail.Tokens.Input != 11 || detail.Tokens.Output != 7 {
		t.Errorf("lost token counts: %+v", detail)
	}
}
