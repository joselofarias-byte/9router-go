package chat

import (
	"9router/proxy/internal/handlerutil"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// HandleFabricProbe is mounted only behind the administrative authenticator.
// It pins a single existing account; a passing probe cannot mask its failure
// by silently falling back to another account. Credentials stay in the gateway.
func (h *ChatHandler) HandleFabricProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string          `json:"connectionId"`
		Model        string          `json:"model"`
		Request      json.RawMessage `json:"request"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || req.ConnectionID == "" || req.Model == "" {
		handlerutil.WriteJSONError(w, 400, "Invalid probe")
		return
	}
	conn, err := h.Repo.GetProviderConnectionByID(req.ConnectionID)
	if err != nil || conn == nil {
		handlerutil.WriteJSONError(w, 404, "Unknown account")
		return
	}
	var payload map[string]any
	if json.Unmarshal(req.Request, &payload) != nil || payload == nil {
		handlerutil.WriteJSONError(w, 400, "Invalid probe payload")
		return
	}
	payload["model"] = req.Model
	stream, _ := payload["stream"].(bool)
	body, err := json.Marshal(payload)
	if err != nil {
		handlerutil.WriteJSONError(w, 400, "Invalid probe payload")
		return
	}
	cw := newCommittedResponseWriter(w)
	if err = h.handleAccountFallback(r.Context(), cw, conn.Provider, req.Model, conn.ID, body, stream, false, "fabric-probe"); err != nil && !cw.IsCommitted() {
		code := 502
		var ue *upstreamError
		if errors.As(err, &ue) && ue.StatusCode >= 400 && ue.StatusCode <= 599 {
			code = ue.StatusCode
		}
		// A provider 404 may describe a route, not a model. Do not label it
		// model_not_found through the generic status-to-error mapping.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
			"message": "Account probe failed", "type": "upstream_error", "code": "account_probe_failed",
		}})
	}
}
