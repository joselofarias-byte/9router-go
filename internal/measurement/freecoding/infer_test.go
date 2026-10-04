package freecoding

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInferenceGateSkipsWithoutCalling(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rec, err := Infer(t.Context(), InferInput{
		Policy: InferencePolicy{
			Flag:          "",
			Provider:      "openrouter",
			ModelID:       "qwen/coder:free",
			Endpoint:      srv.URL,
			APIKey:        "test-free-key",
			Allow:         testAllow(),
			AllowLoopback: true,
		},
		Task: TaskBugfix,
		Run:  1,
		Now:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("request sent despite gate: %d", hits)
	}
	if rec.RecordType != RecordInferSkip || !rec.Skip || !strings.Contains(rec.SkipReason, "FREE_CODING_INFERENCE") {
		t.Fatalf("skip: %+v", rec)
	}
	if strings.Contains(rec.SkipReason, "test-free-key") {
		t.Fatal("secret in skip reason")
	}

	paid := InferenceAllowed(InferencePolicy{
		Flag: "1", Provider: "openrouter", ModelID: "openai/gpt-4o",
		Endpoint: openRouterChatURL, APIKey: "k", Allow: testAllow(),
	})
	if !strings.Contains(paid, ":free") {
		t.Fatalf("paid model: %s", paid)
	}
	openai := InferenceAllowed(InferencePolicy{
		Flag: "1", Provider: "openrouter", ModelID: "qwen/coder:free",
		Endpoint: "https://api.openai.com/v1/chat/completions", APIKey: "k", Allow: testAllow(),
	})
	if !strings.Contains(openai, "refuse endpoint") {
		t.Fatalf("openai endpoint: %s", openai)
	}
	virtual := InferenceAllowed(InferencePolicy{
		Flag: "1", Provider: "openrouter", ModelID: "coding-best-free",
		Endpoint: openRouterChatURL, APIKey: "k", Allow: testAllow(),
	})
	if !strings.Contains(virtual, "virtual profile") {
		t.Fatalf("virtual: %s", virtual)
	}
	if reason := InferenceAllowed(readyPolicy(openRouterChatURL)); reason != "" {
		t.Fatal(reason)
	}
}

func TestInferRecordsOutcomeWithoutSecrets(t *testing.T) {
	pass, err := fixtureFS.ReadFile("testdata/bugfix/pass/sum.go")
	if err != nil {
		t.Fatal(err)
	}
	const secret = "test-free-key"
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("authorization missing")
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
		}
		if payload.Model != "qwen/coder:free" {
			t.Errorf("model %s", payload.Model)
		}
		w.Header().Set("X-Ratelimit-Limit", "20")
		w.Header().Set("X-Ratelimit-Remaining", "19")
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"usage": map[string]any{"cost": 0},
			"choices": []any{map[string]any{"message": map[string]any{
				"content": "```go\n" + string(pass) + "\n```",
			}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	now := time.Date(2026, 10, 1, 3, 4, 5, 0, time.UTC)
	rec, err := Infer(t.Context(), InferInput{
		Policy: readyPolicy(srv.URL),
		Task:   TaskBugfix,
		Run:    1,
		Now:    now,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}
	if !rec.Pass || rec.HTTPStatus != 200 || rec.ReportedCost != "0" || rec.Status429Count != 0 {
		t.Fatalf("record: %+v", rec)
	}
	if rec.QuotaScope != "X-Ratelimit-Limit=20;X-Ratelimit-Remaining=19" {
		t.Fatalf("quota %q", rec.QuotaScope)
	}
	if rec.CompileOK == nil || !*rec.CompileOK || rec.TestOK == nil || !*rec.TestOK {
		t.Fatalf("compile/test %+v", rec)
	}
	raw, _ := json.Marshal(rec)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("secret stored: %s", raw)
	}
}

func TestInferCounts429AndRedacts(t *testing.T) {
	const secret = "test-free-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down test-free-key"}}`)
	}))
	defer srv.Close()
	rec, err := Infer(t.Context(), InferInput{
		Policy: readyPolicy(srv.URL),
		Task:   TaskReview,
		Run:    2,
		Now:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Pass || rec.HTTPStatus != 429 || rec.Status429Count != 1 {
		t.Fatalf("%+v", rec)
	}
	if strings.Contains(rec.Error, secret) || !strings.Contains(rec.Error, "[redacted]") {
		t.Fatalf("error %q", rec.Error)
	}
	if rec.ReportedCost != "" {
		t.Fatalf("cost invented: %q", rec.ReportedCost)
	}
}

func readyPolicy(endpoint string) InferencePolicy {
	return InferencePolicy{
		Flag:          "1",
		Provider:      "openrouter",
		ModelID:       "qwen/coder:free",
		Endpoint:      endpoint,
		APIKey:        "test-free-key",
		Allow:         testAllow(),
		AllowLoopback: true,
	}
}

func testAllow() AllowList {
	return LoadAllowlist([]Record{{
		RecordType:  RecordTrial,
		Provider:    "openrouter",
		ModelID:     "qwen/coder:free",
		PricingMode: "free_tier",
	}})
}
