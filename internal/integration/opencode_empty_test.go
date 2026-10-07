//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func emptyOpencodeEnv(t *testing.T, stream bool) (*Env, *Upstream) {
	t.Helper()
	// Fail before sending any payload if a routing regression ignores the
	// configured fake upstream. This check is local-only by construction.
	originalTransport := http.DefaultTransport
	transport := originalTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("offline test blocked non-loopback destination %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { transport.CloseIdleConnections(); http.DefaultTransport = originalTransport })
	env := newEnv(t)
	var up *Upstream
	up = env.NewUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if up.Last(t).Model(t) == "audit-empty-free" {
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: [DONE]\n\n"))
			} else {
				JSONResponder(200, `{"choices":[{"index":0,"message":{"content":"","reasoning_content":"thinking"},"finish_reason":"stop"}]}`)(w, r)
			}
			return
		}
		if stream {
			streamResponder()(w, r)
			return
		}
		chatCompletionResponder()(w, r)
	})
	env.AddConnection(t, "conn-opencode", "opencode", "Local mock", up, "mock-upstream-key")
	env.AddCombo(t, "combo-empty", "free-best", []string{"opencode/audit-empty-free", "opencode/audit-text-free"})
	return env, up
}

func TestOpencodeEmptyFallbackProductionRouter(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			env, up := emptyOpencodeEnv(t, stream)
			res := env.Post(t, "/v1/chat/completions", ChatBody("free-best", stream))
			if res.Status != http.StatusOK || up.Count() != 2 || !strings.Contains(string(res.Body), "reply") {
				t.Fatalf("empty fallback failed: status=%d requests=%d body=%s", res.Status, up.Count(), res.Body)
			}
			if up.Last(t).Model(t) != "audit-text-free" {
				t.Fatal("did not reach next model on same account")
			}
			if stream && strings.Count(string(res.Body), "[DONE]") != 1 {
				t.Fatal("first empty stream leaked")
			}
		})
	}
}

// Opt-in binary check: production HTTP router, real SQLite and actual OpenCode
// process, with every model request going to the local fixture above.
func TestOpenCodeBinaryEmptyFallback(t *testing.T) {
	bin := os.Getenv("NINE_ROUTER_TEST_OPENCODE_BIN")
	if bin == "" {
		t.Skip("set NINE_ROUTER_TEST_OPENCODE_BIN to a verified OpenCode 1.18.34 binary")
	}
	env, up := emptyOpencodeEnv(t, true)
	folder := t.TempDir()
	model := "router/free-best"
	profile := map[string]any{"model": model, "small_model": model, "provider": map[string]any{
		"router": map[string]any{"npm": "@ai-sdk/openai-compatible", "options": map[string]any{"baseURL": env.BaseURL + "/v1", "apiKey": env.APIKey},
			"models": map[string]any{"free-best": map[string]any{"name": "Free Best", "limit": map[string]int{"context": 32768, "output": 4096}}}},
	}}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(folder, "opencode.json")
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(folder, name))
	}
	t.Setenv("OPENCODE_CONFIG", config)
	t.Setenv("OPENCODE_CONFIG_DIR", folder)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--pure", "--format", "json", "--model", model, "Reply briefly without using tools.")
	cmd.Dir = folder
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenCode failed: %v %s", err, out)
	}
	var visible strings.Builder
	for _, line := range strings.Split(string(out), "\n") {
		var event struct {
			Type string `json:"type"`
			Part struct {
				Text string `json:"text"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "text" {
			visible.WriteString(event.Part.Text)
		}
	}
	// OpenCode can also request a title through small_model. That request
	// should skip the model just put into cooldown rather than retry it.
	if !strings.Contains(visible.String(), "streamed reply") || up.Count() < 2 {
		t.Fatalf("OpenCode did not receive fallback text: requests=%d text=%q output=%s", up.Count(), visible.String(), out)
	}
}
