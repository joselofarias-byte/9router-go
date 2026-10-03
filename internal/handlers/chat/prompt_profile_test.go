package chat

import (
	"context"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/promptprofile"
)

func TestWithRequestPromptProfile(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    string
		wantErr bool
	}{
		{name: "selects built-in", header: promptprofile.OpenAIAgenticV1, want: promptprofile.OpenAIAgenticV1},
		{name: "header case is canonical", header: "OpenAI-Agentic-V1", want: promptprofile.OpenAIAgenticV1},
		{name: "absent header leaves default", header: "", want: ""},
		{name: "none is explicit opt-out", header: "none", want: ""},
		{name: "unknown name is rejected", header: "does-not-exist", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tt.header != "" {
				req.Header.Set(promptprofile.Header, tt.header)
			}
			ctx, err := withRequestPromptProfile(context.Background(), req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got := promptprofile.FromContext(ctx); got != tt.want {
				t.Fatalf("profile=%q want %q", got, tt.want)
			}
		})
	}
}

func TestApplySelectedPromptProfileSkipsUnscopedProvider(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	ctx := promptprofile.WithName(context.Background(), promptprofile.OpenAIAgenticV1)
	got, err := applySelectedPromptProfile(ctx, "anthropic", "claude", body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("unscoped provider body changed: %s", got)
	}
}

func TestApplySelectedPromptProfileCodexKeepsCallerInstruction(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"caller system"},{"role":"user","content":"hello"}]}`)
	ctx := promptprofile.WithName(context.Background(), promptprofile.OpenAIAgenticV1)
	got, err := applySelectedPromptProfile(ctx, "codex", "gpt-test", body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	msgs := decoded["messages"].([]any)
	first := msgs[0].(map[string]any)
	content, _ := first["content"].(string)
	if first["role"] != "system" || !strings.HasPrefix(content, "caller system\n\n") {
		t.Fatalf("first instruction=%v", first)
	}
	if !strings.Contains(content, "You are the engineering agent for this request.") {
		t.Fatalf("profile missing from caller instruction: %s", content)
	}
}

func TestHandleChatCompletionsUnknownPromptProfileReturns400(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	handler := NewChatHandler(db.NewRepo(database))

	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	endpoints := []struct {
		name   string
		path   string
		body   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "chat completions",
			path:   "/v1/chat/completions",
			body:   `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hi"}]}`,
			handle: handler.HandleChatCompletions,
		},
		{
			name:   "messages",
			path:   "/v1/messages",
			body:   `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hi"}]}`,
			handle: handler.HandleMessages,
		},
		{
			name:   "responses",
			path:   "/v1/responses",
			body:   `{"model":"deepseek/deepseek-chat","input":"hi"}`,
			handle: handler.HandleResponses,
		},
	}
	for _, tt := range endpoints {
		t.Run(tt.name, func(t *testing.T) {
			before := hits
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set(promptprofile.Header, "does-not-exist")
			rec := httptest.NewRecorder()
			tt.handle(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "does-not-exist") {
				t.Fatalf("error body=%s", rec.Body.String())
			}
			if hits != before {
				t.Fatal("unknown profile reached an upstream")
			}
		})
	}
}

func TestHandleChatCompletionsInjectsOpenAIProfile(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer upstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedConnDB(t, database, "openai", "conn-prompt-openai", "sk-openai", upstream.URL)
	handler := NewChatHandler(db.NewRepo(database))

	body := `{"model":"openai/gpt-test","messages":[{"role":"system","content":"caller system"},{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set(promptprofile.Header, promptprofile.OpenAIAgenticV1)
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got, `"role":"developer"`) || !strings.Contains(got, "You are the engineering agent for this request.") {
		t.Fatalf("developer profile missing from upstream body: %s", got)
	}
	if !strings.Contains(got, "caller system") || !strings.Contains(got, "hello") {
		t.Fatalf("caller content was dropped: %s", got)
	}
}

func TestHandleChatCompletionsDefaultDoesNotInjectProfile(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer upstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedConnDB(t, database, "openai", "conn-prompt-default", "sk-openai", upstream.URL)
	handler := NewChatHandler(db.NewRepo(database))

	body := `{"model":"openai/gpt-test","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(got, "You are the engineering agent for this request.") || strings.Contains(got, `"role":"developer"`) {
		t.Fatalf("default request injected a profile: %s", got)
	}
}

func TestHandleChatCompletionsCodexPreservesSystemInstruction(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	seedConnDB(t, database, "codex", "conn-prompt-codex", "sk-codex", upstream.URL)
	handler := NewChatHandler(db.NewRepo(database))

	body := `{"model":"codex/gpt-test","messages":[{"role":"system","content":"caller system"},{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set(promptprofile.Header, promptprofile.WorkspaceContextV1)
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got, "caller system") {
		t.Fatalf("caller system instruction was dropped: %s", got)
	}
	if !strings.Contains(got, "You are working inside an existing technical workspace.") {
		t.Fatalf("codex profile missing from upstream instructions: %s", got)
	}
	if strings.Contains(got, `"messages"`) {
		t.Fatalf("codex upstream still received chat messages: %s", got)
	}
}
