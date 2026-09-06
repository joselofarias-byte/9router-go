package proxy

import (
	"strings"
	"testing"
)

func TestUpstreamError_OmitsBody(t *testing.T) {
	e := &UpstreamError{StatusCode: 400, Body: []byte(`{"error":{"message":"Invalid tool parameters"}}`)}
	msg := e.Error()
	if !strings.HasPrefix(msg, "upstream returned 400") {
		t.Errorf("expected prefix 'upstream returned 400', got %q", msg)
	}
	if strings.Contains(msg, "Invalid tool parameters") {
		t.Errorf("private body must not appear in message, got %q", msg)
	}
}

func TestUpstreamError_TruncatesLongBody(t *testing.T) {
	longBody := strings.Repeat("x", 2000)
	e := &UpstreamError{StatusCode: 502, Body: []byte(longBody)}
	msg := e.Error()
	if len(msg) >= 2000 {
		t.Errorf("expected truncated message, got length %d", len(msg))
	}
	if msg != "upstream returned 502" {
		t.Errorf("expected status only, got %q", msg[:min(len(msg), 30)])
	}
}

func TestUpstreamError_EmptyBody(t *testing.T) {
	e := &UpstreamError{StatusCode: 429, Body: nil}
	if msg := e.Error(); msg != "upstream returned 429" {
		t.Errorf("expected 'upstream returned 429', got %q", msg)
	}
}

func TestUpstreamError_WhitespaceBodyTreatedAsEmpty(t *testing.T) {
	e := &UpstreamError{StatusCode: 500, Body: []byte("   \n  ")}
	if msg := e.Error(); msg != "upstream returned 500" {
		t.Errorf("expected 'upstream returned 500' for whitespace body, got %q", msg)
	}
}
