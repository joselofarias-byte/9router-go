package chat

import (
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAntigravityQuota_RefreshAndBlock(t *testing.T) {
	ClearAntigravityQuotaCache()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-ag-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		futureReset := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
		resp := map[string]any{
			"models": map[string]any{
				"gemini-3.7-flash-high": map[string]any{
					"isInternal": false,
					"quotaInfo": map[string]any{
						"remainingFraction": 0.0,
						"resetTime":         futureReset,
					},
				},
				"gemini-3.5-flash-low": map[string]any{
					"isInternal": false,
					"quotaInfo": map[string]any{
						"remainingFraction": 0.85,
						"resetTime":         futureReset,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, resp)
	}))
	defer mockServer.Close()

	oldBase := antigravityQuotaBaseURL
	antigravityQuotaBaseURL = mockServer.URL
	defer func() { antigravityQuotaBaseURL = oldBase }()

	client := mockServer.Client()
	connectionID := "test-ag-conn-1"

	// 1. Initial refresh
	quotas, err := RefreshAntigravityQuota(context.Background(), client, connectionID, "test-ag-token", "test-project")
	if err != nil {
		t.Fatalf("RefreshAntigravityQuota failed: %v", err)
	}

	if len(quotas) != 2 {
		t.Errorf("expected 2 quotas, got %d", len(quotas))
	}

	// 2. Check model block for exhausted model (remaining=0)
	if !IsAntigravityModelBlocked(connectionID, "gemini-3.7-flash-high") {
		t.Errorf("expected gemini-3.7-flash-high to be blocked")
	}

	// 3. Check model block for available model (remaining=85)
	if IsAntigravityModelBlocked(connectionID, "gemini-3.5-flash-low") {
		t.Errorf("expected gemini-3.5-flash-low NOT to be blocked")
	}

	// 4. Handle 429 quota error returns resetAt
	resetAt := HandleAntigravityQuotaError(context.Background(), client, connectionID, 429, "gemini-3.7-flash-high", "test-ag-token", "test-project")
	if resetAt == nil || resetAt.Before(time.Now()) {
		t.Errorf("expected future resetAt from 429 handler, got %v", resetAt)
	}
}

func TestAntigravityQuota_CoalescingAndThrottle(t *testing.T) {
	ClearAntigravityQuotaCache()

	callCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		time.Sleep(50 * time.Millisecond) // simulate delay
		resp := map[string]any{
			"models": map[string]any{
				"gemini-3.7-flash-high": map[string]any{
					"isInternal": false,
					"quotaInfo": map[string]any{
						"remainingFraction": 1.0,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, resp)
	}))
	defer mockServer.Close()

	oldBase := antigravityQuotaBaseURL
	antigravityQuotaBaseURL = mockServer.URL
	defer func() { antigravityQuotaBaseURL = oldBase }()

	client := mockServer.Client()
	connID := "test-ag-conn-throttle"

	// Concurrent refreshes should coalesce to 1 call
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func() {
			_, _ = RefreshAntigravityQuota(context.Background(), client, connID, "tok", "proj")
			done <- true
		}()
	}
	for i := 0; i < 5; i++ {
		<-done
	}

	if callCount != 1 {
		t.Errorf("expected 1 call due to in-flight coalescing, got %d", callCount)
	}

	// Immediate next refresh should hit 30s throttle and not call upstream
	_, _ = RefreshAntigravityQuota(context.Background(), client, connID, "tok", "proj")
	if callCount != 1 {
		t.Errorf("expected callCount to stay 1 due to 30s throttle, got %d", callCount)
	}
}

func TestAntigravityWeeklyQuota_ParseAndFetch(t *testing.T) {
	rawSummary := []byte(`{
		"groups": [
			{
				"displayName": "Gemini Models",
				"buckets": [
					{
						"bucketId": "gemini-weekly-bucket",
						"displayName": "Gemini Weekly Limit",
						"disabled": false,
						"remainingFraction": 0.75,
						"resetTime": "2026-09-17T00:00:00Z"
					}
				]
			},
			{
				"displayName": "Claude & GPT Models",
				"buckets": [
					{
						"bucketId": "claude-gpt-weekly-bucket",
						"displayName": "Claude GPT Weekly",
						"disabled": false,
						"remainingFraction": 0.20,
						"resetTime": "2026-09-17T00:00:00Z"
					}
				]
			}
		]
	}`)

	parsed := ParseWeeklyQuotaSummary(rawSummary)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 weekly quotas, got %d", len(parsed))
	}

	gw, ok := parsed["gemini_weekly"]
	if !ok {
		t.Fatal("expected gemini_weekly")
	}
	if gw.Total != 1000 || gw.Used != 250 || gw.RemainingPercentage != 75.0 {
		t.Errorf("unexpected gemini_weekly values: %+v", gw)
	}

	cw, ok := parsed["claude_gpt_weekly"]
	if !ok {
		t.Fatal("expected claude_gpt_weekly")
	}
	if cw.Total != 1000 || cw.Used != 800 || cw.RemainingPercentage != 20.0 {
		t.Errorf("unexpected claude_gpt_weekly values: %+v", cw)
	}

	// Test FetchAntigravityWeeklyQuota with mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-weekly-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(rawSummary)
	}))
	defer mockServer.Close()

	oldBase := antigravityQuotaBaseURL
	antigravityQuotaBaseURL = mockServer.URL
	defer func() { antigravityQuotaBaseURL = oldBase }()

	ClearAntigravityQuotaCache()
	res, err := FetchAntigravityWeeklyQuota(context.Background(), mockServer.Client(), "test-weekly-token", "proj-1")
	if err != nil {
		t.Fatalf("FetchAntigravityWeeklyQuota failed: %v", err)
	}
	if len(res) != 2 {
		t.Errorf("expected 2 quotas from fetch, got %d", len(res))
	}
}
