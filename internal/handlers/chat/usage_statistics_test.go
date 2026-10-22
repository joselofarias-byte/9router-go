package chat

import (
	json "encoding/json/v2"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/translator"
)

func TestRequestStatisticsPersistCostAndMissingPrice(t *testing.T) {
	tests := []struct {
		name, model string
		priced      bool
	}{
		{"known tariff", "deepseek-chat", true},
		{"unknown tariff is not free", "unknown-statistics-model", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			repo := db.NewRepo(database)
			handler := NewChatHandler(repo)
			info := &UsageLogInfo{Provider: "deepseek", Model: tt.model, ConnectionID: "statistics-account", Endpoint: "/v1/chat/completions"}
			for range 2 {
				handler.LogUsage(info, &translator.OpenAIUsage{PromptTokens: 100, CompletionTokens: 40, CachedTokens: 80}, 2000, nil, nil)
			}
			rows, total, err := repo.GetRequestDetailsPaged(10, 0)
			if err != nil {
				t.Fatal(err)
			}
			if total != 2 || len(rows) != 2 {
				t.Fatalf("recorded %d / %d, want two distinct requests", total, len(rows))
			}
			for _, raw := range rows {
				var detail struct {
					ID              string   `json:"id"`
					Cost            *float64 `json:"cost"`
					CostEstimated   bool     `json:"costEstimated"`
					TokensEstimated bool     `json:"tokensEstimated"`
					Endpoint        string   `json:"endpoint"`
					ConnectionID    string   `json:"connectionId"`
					Latency         struct {
						Total int64 `json:"total"`
					} `json:"latency"`
					Tokens struct {
						Cached int `json:"cached_tokens"`
					} `json:"tokens"`
				}
				if err := json.Unmarshal([]byte(raw), &detail); err != nil {
					t.Fatal(err)
				}
				if (detail.Cost != nil) != tt.priced {
					t.Errorf("cost %v, priced=%v", detail.Cost, tt.priced)
				}
				if tt.priced && detail.Cost != nil && *detail.Cost <= 0 {
					t.Errorf("expected positive cost, got %v", *detail.Cost)
				}
				if detail.ID == "" || !detail.CostEstimated || detail.TokensEstimated {
					t.Errorf("invalid measurement metadata: %+v", detail)
				}
				if detail.Endpoint != info.Endpoint || detail.ConnectionID != info.ConnectionID || detail.Latency.Total != 2000 || detail.Tokens.Cached != 80 {
					t.Errorf("lost request statistics: %+v", detail)
				}
			}
		})
	}
}
