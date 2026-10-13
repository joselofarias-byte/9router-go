package multiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const maxResponseBytes = 1 << 20
var errResponseTooLarge = errors.New("response too large")
var errEmptyResponse = errors.New("empty model response")

// boundedResponse limits the adapter's buffer and cancels provider work on overflow.
type boundedResponse struct {
	header http.Header
	status int
	body bytes.Buffer
	overflow bool
	cancel context.CancelFunc
}

func (w *boundedResponse) Header() http.Header { return w.header }
func (w *boundedResponse) WriteHeader(status int) {
	if w.status == 0 { w.status = status }
}
func (w *boundedResponse) Write(p []byte) (int, error) {
	if w.status == 0 { w.status = http.StatusOK }
	if w.overflow || len(p) > maxResponseBytes - w.body.Len() {
		w.overflow = true
		w.cancel()
		return 0, errResponseTooLarge
	}
	return w.body.Write(p)
}

// HostInvoker uses the existing chat routing, account rotation and usage accounting.
// Requests are non-streaming because each result is returned as one comparison card.
func HostInvoker(handler http.HandlerFunc) Invoker {
	return func(ctx context.Context, model, prompt string) (string, error) {
		if err := ctx.Err(); err != nil { return "", err }
		payload, err := json.Marshal(struct {
			Model string `json:"model"`
			Stream bool `json:"stream"`
			MaxTokens int `json:"max_tokens"`
			Messages []struct { Role string `json:"role"`; Content string `json:"content"` } `json:"messages"`
		}{model, false, 4096, []struct { Role string `json:"role"`; Content string `json:"content"` }{{"user", prompt}}})
		if err != nil { return "", errors.New("invalid request") }
		taskCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		req, err := http.NewRequestWithContext(taskCtx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
		if err != nil { return "", err }
		req.Header.Set("Content-Type", "application/json")
		rec := &boundedResponse{header: make(http.Header), cancel: cancel}
		handler(rec, req)
		if rec.overflow { return "", errResponseTooLarge }
		if err := taskCtx.Err(); err != nil { return "", err }
		if rec.status != http.StatusOK { return "", errors.New("upstream failed") }
		var response struct {
			Choices []struct { Message struct { Content string `json:"content"` } `json:"message"` } `json:"choices"`
		}
		if json.Unmarshal(rec.body.Bytes(), &response) != nil || len(response.Choices) == 0 {
			return "", errors.New("invalid upstream response")
		}
		output := response.Choices[0].Message.Content
		if strings.TrimSpace(output) == "" { return "", errEmptyResponse }
		return output, nil
	}
}
