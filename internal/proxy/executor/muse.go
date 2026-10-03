package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"9router/proxy/internal/proxy"
)

// ForwardMuse serves Meta Muse through its Responses API lane.
// All Muse Spark models currently published by upstream use this transport.
// The dashboard/provider registry supplies x-api-version: 1.0.0.
func ForwardMuse(w http.ResponseWriter, req *Request) error {
	var envelope map[string]any
	if err := json.Unmarshal(req.Body, &envelope); err != nil {
		return fmt.Errorf("ForwardMuse: parse body: %w", err)
	}

	model, _ := envelope["model"].(string)
	model = strings.TrimPrefix(model, "muse/")
	if i := strings.IndexByte(model, '('); i >= 0 {
		model = model[:i]
	}
	if model == "" {
		return fmt.Errorf("ForwardMuse: missing model")
	}
	envelope["model"] = model

	rawBody, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("ForwardMuse: encode body: %w", err)
	}
	body, _, err := buildResponsesBody(rawBody)
	if err != nil {
		return fmt.Errorf("ForwardMuse: transform body: %w", err)
	}
	body, err = normalizeMuseSparkResponsesBody(body, model)
	if err != nil {
		return fmt.Errorf("ForwardMuse: normalize body: %w", err)
	}

	cfg := *req.Config
	base := strings.TrimRight(cfg.BaseURL, "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	base = strings.TrimSuffix(base, "/responses")
	cfg.BaseURL = base + "/responses"

	ctx := req.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Responses is consumed as an event stream even for a non-streaming client;
	// handleCodexStream performs the final aggregation when req.IsStream is false.
	resp, err := proxy.ForwardOpenAI(ctx, req.Client, &cfg, req.APIKey, body, true)
	if err != nil {
		return fmt.Errorf("ForwardMuse: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
		return &proxy.UpstreamError{StatusCode: resp.StatusCode, Body: errBody}
	}

	if req.IsStream {
		stallReader := proxy.NewStallReaderWithContext(ctx, resp.Body, 0, "muse-responses")
		defer stallReader.Close()
		return handleCodexStream(w, req, stallReader)
	}
	return handleCodexStream(w, req, resp.Body)
}
