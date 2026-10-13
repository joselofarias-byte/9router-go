package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancelledRunDoesNotInvoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	for range 50 {
		results := Run(ctx, "task", []string{"a", "b"}, 2, time.Second, func(context.Context, string, string) (string, error) {
			calls.Add(1)
			return "unexpected", nil
		})
		for _, result := range results {
			if result.Error != "cancelled" { t.Fatalf("result: %+v", result) }
		}
	}
	if calls.Load() != 0 { t.Fatalf("cancelled work invoked %d providers", calls.Load()) }
}

func TestZeroConcurrencyDoesNotHang(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	results := Run(ctx, "task", []string{"a"}, 0, time.Second, func(context.Context, string, string) (string, error) { return "ok", nil })
	if results[0].Output != "ok" { t.Fatalf("result: %+v", results) }
}

func TestHostResponseBoundsAndEmptyContent(t *testing.T) {
	tests := []struct { name string; body string; want error }{
		{"empty", `{"choices":[{"message":{"content":""}}]}`, errEmptyResponse},
		{"whitespace", `{"choices":[{"message":{"content":"  "}}]}`, errEmptyResponse},
		{"oversized", strings.Repeat("x", maxResponseBytes+1), errResponseTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := HostInvoker(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tt.body)) })
			_, err := inv(context.Background(), "provider/model", "task")
			if !errors.Is(err, tt.want) { t.Fatalf("error %v, want %v", err, tt.want) }
		})
	}
}

func TestOversizedResponseCancelsAndStopsBufferGrowth(t *testing.T) {
	cancelled := false
	w := &boundedResponse{header: make(http.Header), cancel: func() { cancelled = true }}
	if _, err := w.Write([]byte("initial")); err != nil { t.Fatal(err) }
	if _, err := w.Write(make([]byte, maxResponseBytes)); !errors.Is(err, errResponseTooLarge) { t.Fatalf("error: %v", err) }
	if _, err := w.Write([]byte("more")); !errors.Is(err, errResponseTooLarge) { t.Fatalf("error: %v", err) }
	if !cancelled || w.body.String() != "initial" { t.Fatal("overflow retained data or did not cancel") }
}

func TestHostRequestUsesExactModelAndTokenLimit(t *testing.T) {
	inv := HostInvoker(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
			Stream bool `json:"stream"`
			MaxTokens int `json:"max_tokens"`
			Messages []struct { Content string `json:"content"` } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Error(err); return }
		if body.Model != "provider/exact-model" || body.Stream || body.MaxTokens != 4096 ||
			len(body.Messages) != 1 || body.Messages[0].Content != "exact prompt" { t.Errorf("request: %+v", body) }
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	})
	output, err := inv(context.Background(), "provider/exact-model", "exact prompt")
	if err != nil || output != "answer" { t.Fatalf("output=%q err=%v", output, err) }
}

func TestPolicyPreservesAllowlistAndDisabledState(t *testing.T) {
	t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS", " provider/a,provider/b,provider/a, ")
	t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL", "")
	w := httptest.NewRecorder()
	PolicyHandler(w, httptest.NewRequest(http.MethodGet, "/api/multiagent/policy", nil))
	var p Policy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil { t.Fatal(err) }
	if len(p.Models) != 2 || p.Models[0] != "provider/a" || p.Models[1] != "provider/b" ||
		p.ExternalEnabled || p.MaxModels != 8 || p.MaxOutputTokens != 4096 { t.Fatalf("policy: %+v", p) }
}

func TestTimeoutReportsTimeout(t *testing.T) {
	results := Run(context.Background(), "task", []string{"slow"}, 1, time.Millisecond, func(ctx context.Context, model, prompt string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if results[0].Error != "timed out" { t.Fatalf("result: %+v", results) }
}
