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

type FabricProber struct {
	h *ChatHandler
}

func NewFabricProber(h *ChatHandler) *FabricProber {
	return &FabricProber{h: h}
}

func probeRequestBody(model string) []byte {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	})
	return body
}

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

// HandleFabricProbe runs an on-demand verification probe.
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

	if result.Success {
		recordRouteOutcome(provider, model, account, true, 0, nil, result.LatencyMs)
	} else {
		recordRouteOutcome(provider, model, account, false, 0, []byte(result.ErrorMessage), result.LatencyMs)
	}
	handlerutil.WriteJSON(w, http.StatusOK, result)
}
