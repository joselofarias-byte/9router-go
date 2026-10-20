package chat

import (
    "errors"
    "strings"
    "testing"

    "9router/proxy/internal/db"
    "9router/proxy/internal/translator"
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

func TestLogFailureDefaultDoesNotPersistPromptsOrUpstreamErrors(t *testing.T) {
    t.Setenv("CAPIMUX_CAPTURE_REQUEST_CONTENT", "")
    database, cleanup := setupChatTestDB(t)
    defer cleanup()
    repo := db.NewRepo(database)
    handler := NewChatHandler(repo)
    handler.LogFailure(
        &UsageLogInfo{Provider: "openai", Model: "grok004", ConnectionID: "test"},
        nil,
        errors.New("grok004-sensitive-upstream-error-canary"),
        12,
        []byte(`{"model":"grok004","messages":[{"role":"user","content":"grok004-private-prompt-canary"}]}`),
        nil,
    )
    var raw string
    if err := database.QueryRow(`SELECT data FROM requestDetails WHERE model = 'grok004'`).Scan(&raw); err != nil {
        t.Fatal(err)
    }
    for _, canary := range []string{"grok004-private-prompt-canary", "grok004-sensitive-upstream-error-canary"} {
        if strings.Contains(raw, canary) {
            t.Fatalf("sensitive content persisted: %s", canary)
        }
    }
    if !strings.Contains(raw, "request failed") {
        t.Fatalf("missing safe failure summary: %s", raw)
    }
}

func TestLogUsageDefaultDoesNotPersistSuccessfulPromptContent(t *testing.T) {
    t.Setenv("CAPIMUX_CAPTURE_REQUEST_CONTENT", "")
    database, cleanup := setupChatTestDB(t)
    defer cleanup()
    repo := db.NewRepo(database)
    handler := NewChatHandler(repo)
    handler.LogUsage(
        &UsageLogInfo{Provider: "openai", Model: "grok004-success", ConnectionID: "test"},
        &translator.OpenAIUsage{PromptTokens: 10, CompletionTokens: 5},
        12,
        []byte(`{"model":"grok004-success","messages":[{"role":"user","content":"grok004-private-success-canary"}]}`),
        nil,
    )
    var raw string
    if err := database.QueryRow(`SELECT data FROM requestDetails WHERE model = 'grok004-success'`).Scan(&raw); err != nil {
        t.Fatal(err)
    }
    if strings.Contains(raw, "grok004-private-success-canary") {
        t.Fatal("successful request content persisted by default")
    }
    if !strings.Contains(raw, "prompt_tokens") {
        t.Fatalf("usage metrics missing after privacy control: %s", raw)
    }
}
