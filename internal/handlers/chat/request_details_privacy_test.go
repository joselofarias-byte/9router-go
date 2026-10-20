package chat

import (
    "testing"
)

func TestRequestDetailsCaptureOptIn(t *testing.T) {
    t.Setenv("CAPIMUX_CAPTURE_REQUEST_CONTENT", "")
    if captureRequestDetailContent() {
        t.Fatal("request content must not be recorded by default")
    }
    t.Setenv("CAPIMUX_CAPTURE_REQUEST_CONTENT", "false")
    if captureRequestDetailContent() {
        t.Fatal("explicit false must disable content capture")
    }
    t.Setenv("CAPIMUX_CAPTURE_REQUEST_CONTENT", "garbage")
    if captureRequestDetailContent() {
        t.Fatal("invalid flag must fail closed")
    }
    t.Setenv("CAPIMUX_CAPTURE_REQUEST_CONTENT", "true")
    if !captureRequestDetailContent() {
        t.Fatal("explicit opt-in must enable content capture")
    }
}
