package chat

import (
    json "encoding/json/v2"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "9router/proxy/internal/db"
)

// Tests the regular Capimux request path without a Magnitude installation.
// A mock HTTP endpoint represents the local OpenAI-compatible engine.
func TestMagnitude_ChatCompletionsMockUpstream(t *testing.T) {
    database, cleanup := setupChatTestDB(t)
    defer cleanup()

    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            t.Errorf("unexpected method: %s", r.Method)
        }
        var request struct {
            Model string `json:"model"`
            Messages []struct { Role string `json:"role"`; Content string `json:"content"` } `json:"messages"`
        }
        if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
            t.Errorf("invalid forwarded request: %v", err)
        }
        if request.Model != "test-model" {
            t.Errorf("upstream model = %q, want test-model", request.Model)
        }
        if len(request.Messages) != 1 || request.Messages[0].Content != "hello" {
            t.Errorf("unexpected forwarded messages: %+v", request.Messages)
        }
        w.Header().Set("Content-Type", "application/json")
        _, _ = io.WriteString(w, `{"id":"mock-magnitude","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
    }))
    defer upstream.Close()

    data, err := json.Marshal(map[string]any{
        "apiKey": "local",
        "baseUrl": upstream.URL,
    })
    if err != nil {
        t.Fatal(err)
    }
    _, err = database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
        VALUES ('conn-magnitude-mock', 'magnitude', 'apikey', 'Mock Magnitude', 1, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, string(data))
    if err != nil {
        t.Fatal(err)
    }

    handler := NewChatHandler(db.NewRepo(database))
    req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"magnitude/test-model","messages":[{"role":"user","content":"hello"}],"stream":false}`))
    rec := httptest.NewRecorder()
    handler.HandleChatCompletions(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
    }
    var response struct {
        Choices []struct { Message struct { Content string `json:"content"` } `json:"message"` } `json:"choices"`
    }
    if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
        t.Fatalf("invalid response: %v", err)
    }
    if len(response.Choices) != 1 || response.Choices[0].Message.Content != "OK" {
        t.Fatalf("unexpected response: %s", rec.Body.String())
    }
}
