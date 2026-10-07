package chat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestUsableResponseWriter(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		stream, valid bool
	}{
		{"empty JSON", `{"choices":[{"message":{"content":""}}]}`, false, false},
		{"reasoning only", `{"choices":[{"message":{"reasoning_content":"thinking"}}]}`, false, false},
		{"text", `{"choices":[{"message":{"content":"ok"}}]}`, false, true},
		{"tool", `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","function":{"name":"read"}}]}}]}`, false, true},
		{"SSE empty", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: [DONE]\n\n", true, false},
		{"SSE text", "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\r\n\r\ndata: [DONE]\n\n", true, true},
		{"SSE tool", "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c\"}]}}]}\n\ndata: [DONE]\n\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			cw := newCommittedResponseWriter(rec)
			w := newUsableResponseWriter(cw)
			ct := "application/json"
			if tc.stream {
				ct = "text/event-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(200)
			w.Flush()
			if cw.IsCommitted() {
				t.Fatal("headers committed before content")
			}
			// Split every byte, including UTF-8 and SSE framing boundaries.
			for _, b := range []byte(tc.body) {
				if _, err := w.Write([]byte{b}); err != nil {
					t.Fatal(err)
				}
			}
			err := w.finish()
			if tc.valid {
				if err != nil || !cw.IsCommitted() || rec.Body.String() != tc.body {
					t.Fatalf("lost valid response: %v %s", err, rec.Body.String())
				}
			} else {
				var ue *upstreamError
				if !errors.As(err, &ue) || ue.StatusCode != 502 || cw.IsCommitted() || rec.Body.Len() != 0 {
					t.Fatalf("empty response committed: %v %s", err, rec.Body.String())
				}
			}
		})
	}
}

func TestComboFallbackSkipsEmptyOpencodeCompletion(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			var models []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Unknown test model IDs keep this on the OpenAI chat endpoint.
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				empty := strings.Contains(string(body), "audit-empty-free")
				models = append(models, string(body))
				text := "fallback-ok"
				if empty {
					text = ""
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
					w.(http.Flusher).Flush()
					w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + text + "\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"choices":[{"message":{"content":"` + text + `"},"finish_reason":"stop"}]}`))
				}
			}))
			defer upstream.Close()
			database, cleanup := setupChatTestDB(t)
			defer cleanup()
			if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
				t.Fatal(err)
			}
			seedConnDB(t, database, "opencode", "conn-empty-test", "mock-key", upstream.URL)
			orig := providers.KnownProviders["opencode"]
			cfg := orig
			cfg.BaseURL = upstream.URL
			providers.KnownProviders["opencode"] = cfg
			defer func() { providers.KnownProviders["opencode"] = orig }()
			h := NewChatHandler(db.NewRepo(database))
			rec := httptest.NewRecorder()
			h.handleComboFallback(context.Background(), rec, []byte(`{"model":"free-best","messages":[{"role":"user","content":"hi"}]}`), []string{"opencode/audit-empty-free", "opencode/audit-text-free"}, "fallback", stream, false, "audit-usable", 0)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "fallback-ok") || len(models) != 2 {
				t.Fatalf("fallback failed: requests=%d code=%d body=%s", len(models), rec.Code, rec.Body.String())
			}
			if stream && strings.Count(rec.Body.String(), "[DONE]") != 1 {
				t.Fatal("leaked first stream into fallback")
			}
		})
	}
}
