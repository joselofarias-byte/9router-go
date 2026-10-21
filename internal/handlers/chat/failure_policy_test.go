package chat

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"testing"
)

func emptyAsError() error { return emptyCompletionError() }

func TestUpstreamHopPlan(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		decision string
		reason   string
		lock     bool
	}{
		{"nil", nil, "stop", "caller_canceled", false},
		{"caller canceled", context.Canceled, "stop", "caller_canceled", false},
		{"401", &upstreamError{StatusCode: http.StatusUnauthorized}, "next_account", "auth", true},
		{"403", &upstreamError{StatusCode: http.StatusForbidden}, "next_account", "forbidden", true},
		{"402", &upstreamError{StatusCode: http.StatusPaymentRequired}, "next_account", "quota", true},
		{"429", &upstreamError{StatusCode: http.StatusTooManyRequests}, "next_account", "rate_limit", true},
		{"400", &upstreamError{StatusCode: http.StatusBadRequest}, "next_model", "permanent_request", false},
		{"404", &upstreamError{StatusCode: http.StatusNotFound}, "next_model", "permanent_request", false},
		{"408", &upstreamError{StatusCode: http.StatusRequestTimeout}, "next_account", "timeout", false},
		{"500", &upstreamError{StatusCode: http.StatusInternalServerError}, "next_account", "upstream_500", false},
		{"503", &upstreamError{StatusCode: http.StatusServiceUnavailable}, "next_account", "unavailable", true},
		{"empty", emptyAsError(), "next_model", "empty_completion", false},
		{"deadline", context.DeadlineExceeded, "next_account", "timeout", false},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "next_account", "transport", false},
		{"plain", errors.New("bug"), "next_model", "unstructured", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, reason, lock := upstreamHopPlan(tt.err)
			if decision != tt.decision || reason != tt.reason || lock != tt.lock {
				t.Fatalf("plan(%s) = %s %s lock=%v, want %s %s lock=%v", tt.name, decision, reason, lock, tt.decision, tt.reason, tt.lock)
			}
		})
	}
}
