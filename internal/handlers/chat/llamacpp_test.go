package chat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

func TestLlamaCpp_StreamToolCalls_NoAuthAlias(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var sawAuth string
	var sawModel string
	var sawTool string
	var sawStream bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream json: %v", err)
		}
		sawModel, _ = req["model"].(string)
		sawStream, _ = req["stream"].(bool)
		if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
			if tool, ok := tools[0].(map[string]any); ok {
				if fn, ok := tool["function"].(map[string]any); ok {
					sawTool, _ = fn["name"].(string)
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-local\",\"model\":\"qwen2.5-coder-7b.gguf\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-local\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"main.go\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	connData, _ := json.Marshal(map[string]any{
		"baseUrl": upstream.URL + "/v1/chat/completions",
	})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-llama', 'llamacpp', 'apikey', 'Local llama-server', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, string(connData)); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	handler := NewChatHandler(db.NewRepo(database))
	reqBody := `{"model":"llama.cpp/qwen2.5-coder-7b.gguf","messages":[{"role":"user","content":"read main"}],"stream":true,"tools":[{"type":"function","function":{"name":"read_file","description":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if sawAuth != "" {
		t.Fatalf("NoAuth local request sent Authorization %q", sawAuth)
	}
	if sawModel != "qwen2.5-coder-7b.gguf" {
		t.Fatalf("model forwarded as %q", sawModel)
	}
	if !sawStream {
		t.Fatal("stream flag was not forwarded")
	}
	if sawTool != "read_file" {
		t.Fatalf("tool name forwarded as %q", sawTool)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"call_read", "read_file", "main.go", "[DONE]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q: %s", want, body)
		}
	}
}

func TestLlamaCpp_SendsRealLoopbackKeyOnly(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var sawAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer upstream.Close()

	connData, _ := json.Marshal(map[string]any{
		"apiKey":  "llama-server-secret",
		"baseUrl": upstream.URL + "/v1/chat/completions",
	})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-llama-key', 'llamacpp', 'apikey', 'Keyed llama-server', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, string(connData)); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	handler := NewChatHandler(db.NewRepo(database))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gguf/qwen2.5-coder","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if sawAuth != "Bearer llama-server-secret" {
		t.Fatalf("local server key auth = %q", sawAuth)
	}
	if !strings.Contains(rec.Body.String(), `"finish_reason":"tool_calls"`) {
		t.Fatalf("non-stream tool_calls not passed through: %s", rec.Body.String())
	}
}

func TestLlamaCpp_CloudBaseURLAndRelayNeverDial(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create proxyPools: %v", err)
	}
	repo := db.NewRepo(database)
	pool, err := repo.InsertProxyPool(db.ProxyPoolData{
		Name:     "vercel-relay",
		ProxyURL: "https://my-relay.vercel.app",
		Type:     "vercel",
	})
	if err != nil {
		t.Fatalf("insert pool: %v", err)
	}
	poolID := pool["id"].(string)
	handler := NewChatHandler(repo)

	_, err = handler.GetProviderConfig("llamacpp", &ConnectionData{
		APIKey:  "sk-should-not-leak",
		BaseURL: "https://api.openai.com/v1/chat/completions",
	})
	if err == nil || !strings.Contains(err.Error(), "local-only") {
		t.Fatalf("expected local-only refusal, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "sk-should-not-leak") {
		t.Fatalf("config error leaked key: %v", err)
	}

	cfg, err := handler.GetProviderConfig("llamacpp", &ConnectionData{ProxyPoolID: poolID})
	if err != nil {
		t.Fatalf("relay config: %v", err)
	}
	if cfg.BaseURL != "http://127.0.0.1:8080/v1/chat/completions" {
		t.Fatalf("edge relay rewrote local base URL to %s", cfg.BaseURL)
	}
	if _, ok := cfg.StaticHeaders["x-relay-target"]; ok {
		t.Fatal("local provider inherited an edge relay target")
	}

	// A loopback override on a cloud provider must also stay on-device.
	cfg, err = handler.GetProviderConfig("openai", &ConnectionData{
		BaseURL:     "http://127.0.0.1:8080/v1/chat/completions",
		ProxyPoolID: poolID,
	})
	if err != nil {
		t.Fatalf("openai loopback override: %v", err)
	}
	if cfg.BaseURL != "http://127.0.0.1:8080/v1/chat/completions" {
		t.Fatalf("loopback openai URL was relayed to %s", cfg.BaseURL)
	}

	connData, _ := json.Marshal(map[string]string{
		"apiKey":  "sk-should-not-leak",
		"baseUrl": "https://api.openai.com/v1/chat/completions",
	})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-bad', 'llamacpp', 'apikey', 'Bad', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, string(connData)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"lc/qwen","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("cloud base URL was accepted: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-should-not-leak") {
		t.Fatalf("error body leaked key: %s", rec.Body.String())
	}
}

func TestGetBestConnection_LlamaCppNoAuth(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	handler := NewChatHandler(db.NewRepo(database))
	conn, data, err := handler.GetBestConnection("llamacpp", "", nil, "qwen2.5-coder-7b.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if conn.ID != "noauth" || data.AccessToken != "public" {
		t.Fatalf("expected virtual noauth connection, got %+v %+v", conn, data)
	}
	cfg, err := handler.GetProviderConfig("llamacpp", data)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoAuth || !cfg.LocalOnly {
		t.Fatalf("virtual connection lost local flags: %+v", cfg)
	}
	if providers.KnownProviders["llamacpp"].BaseURL != cfg.BaseURL {
		t.Fatalf("virtual connection changed base URL to %s", cfg.BaseURL)
	}
}
