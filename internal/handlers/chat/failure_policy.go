package chat

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// upstreamHopPlan classifies one failed upstream attempt.
//
// decision is "next_account", "next_model", or "stop".
// lockAccount is true only for credential, quota, and gateway statuses that
// must cool down the account. 500, timeouts, and transport errors rotate
// without a quota lock. Callers must already have rejected a committed
// response and a canceled caller context before acting on "next_account".
func upstreamHopPlan(err error) (decision, reason string, lockAccount bool) {
	if err == nil || errors.Is(err, context.Canceled) {
		return "stop", "caller_canceled", false
	}
	var ue *upstreamError
	if errors.As(err, &ue) {
		if decision, reason, lockAccount, ok := statusHopPlan(ue); ok {
			return decision, reason, lockAccount
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || netTimeout(err) {
		return "next_account", "timeout", false
	}
	if accountTransientFailure(err) {
		return "next_account", "transport", false
	}
	return "next_model", "unstructured", false
}

func statusHopPlan(ue *upstreamError) (decision, reason string, lockAccount, ok bool) {
	if ue == nil {
		return "", "", false, false
	}
	if isEmptyCompletionError(ue) {
		return "next_model", "empty_completion", false, true
	}
	switch ue.StatusCode {
	case http.StatusUnauthorized:
		return "next_account", "auth", true, true
	case http.StatusForbidden:
		return "next_account", "forbidden", true, true
	case http.StatusPaymentRequired:
		return "next_account", "quota", true, true
	case http.StatusTooManyRequests:
		return "next_account", "rate_limit", true, true
	case http.StatusRequestTimeout:
		return "next_account", "timeout", false, true
	case http.StatusInternalServerError:
		return "next_account", "upstream_500", false, true
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return "next_account", "bad_gateway", true, true
	case http.StatusServiceUnavailable:
		return "next_account", "unavailable", true, true
	default:
		if ue.StatusCode >= 500 {
			return "next_account", "upstream_5xx", false, true
		}
		if ue.StatusCode >= 400 {
			return "next_model", "permanent_request", false, true
		}
	}
	return "", "", false, false
}

func netTimeout(err error) bool {
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}
