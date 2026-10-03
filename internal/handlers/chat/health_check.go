package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/handlerutil"
)

// probeWriter keeps a bounded diagnostic body. It is not a streaming writer
// and does not expose model output or credentials to the caller.
type probeWriter struct {
	header http.Header
	status int
	body   []byte
}

func (w *probeWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *probeWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *probeWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if len(w.body)+len(p) > 64<<10 {
		return 0, io.ErrShortBuffer
	}
	w.body = append(w.body, p...)
	return len(p), nil
}

// HandleAdminHealthCheck sends one bounded text request through the existing
// executor. It accepts only a currently eligible free catalog route, never a
// paid model, and never returns upstream text, errors, or secrets.
func (h *ChatHandler) HandleAdminHealthCheck(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Model string `json:"model"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Expected a model field")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF || !strings.Contains(input.Model, "/") {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Expected one concrete provider/model")
		return
	}
	info, err := h.ResolveModel(input.Model)
	if err != nil || info == nil || info.VirtualFree || !h.freeEntryStillEligible(info.Provider+"/"+info.Model) {
		writeFreeRouteUnavailable(w)
		return
	}
	conn, data, err := h.getBestConnection(info.Provider, info.ConnectionID, nil, info.Model)
	if err != nil || data == nil {
		writeFreeRouteUnavailable(w)
		return
	}
	connectionID := ""
	if conn != nil {
		connectionID = conn.ID
	}
	if !h.freeEntryStillEligible(info.Provider + "/" + info.Model) {
		writeFreeRouteUnavailable(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ctx = withFreeProfile(ctx, "free-best", true)
	body, _ := json.Marshal(map[string]any{"model": info.Model, "stream": false, "max_tokens": 16,
		"messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}})
	out := &probeWriter{header: make(http.Header)}
	start := time.Now()
	err = h.tryForwardWithConnection(forwardRequestParams{
		Ctx: ctx, W: out, Provider: info.Provider, Model: info.Model,
		ConnectionID: connectionID, ConnData: data, Body: body,
		IsStream: false, TranslateResponse: false, Endpoint: "/v1/chat/completions",
	})
	healthy := healthyProbe(err, out)
	observationErr := err
	if !healthy && observationErr == nil {
		observationErr = errors.New("invalid health-check response")
	}
	recordVirtualFreeTraffic(ctx, info.Provider, info.Model, connectionID, observationErr, int(time.Since(start).Milliseconds()), 0)
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	handlerutil.WriteJSON(w, status, map[string]any{
		"provider": info.Provider, "model": info.Model,
		"healthy": healthy, "latencyMs": time.Since(start).Milliseconds(), "probe": "bounded_text",
		"capabilitiesVerified": false,
	})
}

func healthyProbe(err error, out *probeWriter) bool {
	if err != nil || out == nil || (out.status != 0 && out.status != http.StatusOK) {
		return false
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(out.body, &response) != nil || len(response.Choices) == 0 {
		return false
	}
	return strings.TrimSpace(response.Choices[0].Message.Content) != ""
}
