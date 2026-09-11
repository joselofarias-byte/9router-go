package chat

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"9router/proxy/internal/controlplane/verification"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/providers"
)

// FabricProber implements verification.Prober by reusing the exact same
// single-connection dispatch path (tryForwardWithConnection) that live
// combo-fallback traffic uses for every real request. Probing through the
// production dispatch path — rather than a hand-rolled per-provider HTTP
// client — means a probe result reflects the same auth, transform and error
// classification behavior a real request would see, across every provider
// this proxy already supports, without duplicating that logic here.
type FabricProber struct {
	h *ChatHandler
}

// NewFabricProber builds a Prober bound to h's connections/credentials.
func NewFabricProber(h *ChatHandler) *FabricProber {
	return &FabricProber{h: h}
}

// probeRequestBody is a minimal, cheap chat-completion payload used only to
// verify that a specific provider/model/account combination is reachable and
// returns a valid response. It is never shown to a user and never part of a
// real conversation.
func probeRequestBody(model string) []byte {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	})
	return body
}

// probeResponseModel is the minimal shape needed to read back the upstream's
// reported model id for fingerprint comparison; every other response field is
// ignored.
type probeResponseModel struct {
	Model string `json:"model"`
}

func extractResponseModel(body []byte) string {
	var v probeResponseModel
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return v.Model
}

// Probe verifies a specific provider/model/account combination outside the
// hot path: it fetches that exact account's credentials, sends a minimal
// probe request through the same dispatch used for real traffic, and reports
// latency, success/failure classification, and whether the upstream's
// reported model id matches what was requested.
func (p *FabricProber) Probe(ctx context.Context, providerID, modelID, accountID string) (verification.ProbeResult, error) {
	result := verification.ProbeResult{
		Timestamp:  time.Now().UTC(),
		ProviderID: providerID,
		ModelID:    modelID,
		AccountID:  accountID,
	}

	_, connData, err := p.h.getBestConnection(providerID, accountID, nil, modelID)
	if err != nil {
		result.ErrorCategory = providers.ErrPermanent
		result.ErrorMessage = err.Error()
		return result, nil
	}

	rec := httptest.NewRecorder()
	start := time.Now()
	fwdErr := p.h.tryForwardWithConnection(ctx, rec, providerID, modelID, accountID, connData, probeRequestBody(modelID), false, true, "/v1/chat/completions")
	result.LatencyMs = int(time.Since(start).Milliseconds())
	// Non-streaming probe: time-to-first-token and total latency coincide.
	result.TTFTMs = result.LatencyMs

	if fwdErr != nil {
		statusCode := 0
		errText := fwdErr.Error()
		var ue *upstreamError
		if errors.As(fwdErr, &ue) {
			statusCode = ue.StatusCode
			if extracted := extractErrorText(ue.Body); extracted != "" {
				errText = extracted
			}
		}
		cls := providers.ClassifyError(statusCode, errText, 0)
		result.Success = false
		result.ErrorCategory = cls.Category
		result.ErrorMessage = errText
		return result, nil
	}

	result.Success = true
	respModel := extractResponseModel(rec.Body.Bytes())
	result.FingerprintMatches = verification.Fingerprint(modelID, respModel, rec.Body.String())
	result.CapabilitiesVerified = map[string]bool{"chat": true}
	return result, nil
}

// HandleFabricProbe runs an on-demand verification probe against one exact
// provider/model/account combination and feeds the outcome back into the
// same shared trust manager that drives live routing/scoring, so a manual
// probe has the same effect on future routing as an organically observed
// request would. It intentionally runs synchronously and only on request —
// there is no background probing loop, consistent with 9router's on-demand
// execution model.
// POST /admin/fabric/probe?provider=&model=&account=
func (h *ChatHandler) HandleFabricProbe(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")
	account := r.URL.Query().Get("account")
	if provider == "" || model == "" || account == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "provider, model, and account query parameters are required")
		return
	}

	result, err := NewFabricProber(h).Probe(r.Context(), provider, model, account)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	globalTrustManager.RecordObservation(provider, model, account, result.Success, result.ErrorCategory)
	if result.LatencyMs > 0 {
		globalTrustManager.RecordLatency(provider, model, account, result.LatencyMs)
	}

	handlerutil.WriteJSON(w, http.StatusOK, result)
}
