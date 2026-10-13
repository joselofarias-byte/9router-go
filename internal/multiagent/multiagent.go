package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

type Request struct {
	Prompt        string   `json:"prompt"`
	Models        []string `json:"models"`
	Concurrency   int      `json:"concurrency"`
	TimeoutMS     int      `json:"timeout_ms"`
	AllowExternal bool     `json:"allow_external"`
	DryRun        bool     `json:"dry_run"`
}

type Result struct {
	Model      string `json:"model"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type Response struct {
	Results []Result `json:"results"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

type Invoker func(context.Context, string, string) (string, error)

// Admission control bounds concurrent batches across dashboard requests.
var batchSlots = make(chan struct{}, 2)

// Run preserves selection order and cancels queued work before invoking providers.
func Run(ctx context.Context, prompt string, models []string, concurrency int, timeout time.Duration, invoke Invoker) []Result {
	concurrency = max(1, min(concurrency, 8))
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	results := make([]Result, len(models))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, model := range models {
		wg.Add(1)
		go func(i int, model string) {
			defer wg.Done()
			results[i] = Result{Model: model}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i].Error = "cancelled"
				return
			}
			taskCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if taskCtx.Err() != nil {
				results[i].Error = "cancelled"
				return
			}
			start := time.Now()
			output, err := invoke(taskCtx, model, prompt)
			results[i].DurationMS = time.Since(start).Milliseconds()
			results[i].Error = resultError(taskCtx, err)
			if results[i].Error == "" {
				results[i].Output = output
			}
		}(i, model)
	}
	wg.Wait()
	return results
}

func resultError(ctx context.Context, err error) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, errResponseTooLarge):
		return "response too large"
	case errors.Is(err, errEmptyResponse):
		return "empty model response"
	case err != nil:
		return "model request failed"
	default:
		return ""
	}
}

// Handler must be mounted behind admin session or local CLI authentication.
func Handler(invoke Invoker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeRequest(w, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		policy := currentPolicy()
		for _, model := range req.Models {
			if !policy.allows(model) {
				writeError(w, http.StatusForbidden, "model not allowed by server policy")
				return
			}
		}
		if req.DryRun {
			results := make([]Result, len(req.Models))
			for i, model := range req.Models {
				results[i] = Result{Model: model}
			}
			writeResponse(w, Response{Results: results, DryRun: true})
			return
		}
		if !req.AllowExternal || !policy.ExternalEnabled {
			writeError(w, http.StatusForbidden, "external multiagent dispatch disabled or not authorized")
			return
		}
		select {
		case batchSlots <- struct{}{}:
			defer func() { <-batchSlots }()
		default:
			writeError(w, http.StatusTooManyRequests, "multiagent capacity reached")
			return
		}
		concurrency := req.Concurrency
		if concurrency == 0 {
			concurrency = 3
		}
		timeout := time.Duration(req.TimeoutMS) * time.Millisecond
		writeResponse(w, Response{Results: Run(r.Context(), req.Prompt, req.Models, concurrency, timeout, invoke)})
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{message})
}

func writeResponse(w http.ResponseWriter, response Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
