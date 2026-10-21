package chat

import (
	"context"
	"errors"
	"net/http"
	"time"

	"9router/proxy/internal/log"
)

// comboAttempt is one provider hop inside a combo or virtual free pool.
type comboAttempt struct {
	Ctx            context.Context
	CW             *committedResponseWriter
	Err            error
	VirtualFree    bool
	NoAuthProvider bool
	Entry          string
	ConnID         string
	Provider       string
	Model          string
	ExcludeIDs     *[]string
}

// comboAttemptResult tells the combo loop whether to stop, leave this model,
// or try another account of the same model.
type comboAttemptResult struct {
	StopRequest bool
	NextModel   bool
	Upstream    *upstreamError
}

// finishComboAttempt applies the shared failure policy. It never logs upstream
// bodies or credentials: only status, decision, and a stable reason.
func (h *ChatHandler) finishComboAttempt(a comboAttempt) comboAttemptResult {
	if a.CW != nil && a.CW.IsCommitted() {
		log.Error("combo", "upstream error after response started",
			"provider", a.Provider, "model", a.Model, "reason", "partial_delivery")
		return comboAttemptResult{StopRequest: true}
	}
	if a.Ctx != nil && a.Ctx.Err() != nil {
		return comboAttemptResult{NextModel: true, Upstream: clientClosedUpstream()}
	}

	decision, reason, lock := upstreamHopPlan(a.Err)
	var ue *upstreamError
	errors.As(a.Err, &ue)
	status := 0
	if ue != nil {
		status = ue.StatusCode
	}
	if a.VirtualFree {
		log.Info("routing", "virtual free hop decision",
			"entry", a.Entry, "provider", a.Provider, "status", status,
			"decision", decision, "reason", reason)
	}
	if reason == "empty_completion" && ue != nil {
		h.noteEmptyComboCompletion(a)
		return comboAttemptResult{NextModel: true, Upstream: ue}
	}
	if ue != nil && a.VirtualFree && a.NoAuthProvider && quarantineUnsupportedVirtualFreeModel(a.Entry, ue) {
		return comboAttemptResult{NextModel: true, Upstream: ue}
	}
	if ue != nil && lock && a.ExcludeIDs != nil {
		h.comboLockRetryable(a.ExcludeIDs, a.ConnID, a.Provider, a.Model, ue)
	} else if decision == "next_account" && a.ExcludeIDs != nil && a.ConnID != "" {
		*a.ExcludeIDs = append(*a.ExcludeIDs, a.ConnID)
	}
	if decision == "stop" {
		return comboAttemptResult{StopRequest: true, Upstream: ue}
	}
	if decision == "next_model" {
		return comboAttemptResult{NextModel: true, Upstream: coalesceUpstream(ue)}
	}
	logTransientFailover(reason, status, a.Provider)
	return comboAttemptResult{Upstream: coalesceUpstream(ue)}
}

func logTransientFailover(reason string, status int, provider string) {
	switch reason {
	case "unavailable", "bad_gateway", "timeout", "transport", "upstream_500", "upstream_5xx":
		log.Info("combo", "transient failover skip", "status", status, "provider", provider, "reason", reason)
	}
}

func (h *ChatHandler) noteEmptyComboCompletion(a comboAttempt) {
	if h != nil && h.Repo != nil && a.ConnID != "" {
		if err := h.Repo.LockConnectionModel(a.ConnID, a.Model, 30, 0); err != nil {
			log.Warn("combo", "empty model lock failed", "error", err)
		}
	}
	if !a.VirtualFree || !a.NoAuthProvider {
		return
	}
	now := time.Now().UTC()
	virtualFreeModelQuarantine.Store(a.Entry, virtualFreeQuarantineEntry{
		quarantinedAt: now,
		until:         now.Add(30 * time.Second),
	})
}

func clientClosedUpstream() *upstreamError {
	return &upstreamError{
		StatusCode: StatusClientClosedRequest,
		Body:       []byte(`{"error":{"message":"client closed request","type":"client_closed_request","code":499}}`),
	}
}

func coalesceUpstream(ue *upstreamError) *upstreamError {
	if ue != nil {
		return ue
	}
	return &upstreamError{
		StatusCode: http.StatusBadGateway,
		Body:       []byte(`{"error":{"message":"upstream transport failure","type":"upstream_error","code":502}}`),
	}
}
