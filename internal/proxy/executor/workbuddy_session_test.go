package executor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"9router/proxy/internal/proxy"
)

func TestWorkBuddyPromptFromBody(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-luna",
		"messages":[
			{"role":"system","content":"Be concise."},
			{"role":"user","content":[{"type":"text","text":"Say hello"}]},
			{"role":"assistant","content":"Hello"},
			{"role":"user","content":"Again"}
		]
	}`)
	model, prompt, err := workBuddyPromptFromBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "gpt-5.6-luna" {
		t.Fatalf("model=%q", model)
	}
	for _, want := range []string{"[system]\nBe concise.", "[user]\nSay hello", "[assistant]\nHello", "[user]\nAgain"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestWorkBuddyPromptRejectsToolsAndImages(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function"}]}`),
		[]byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`),
		[]byte(`{"model":"gpt-5.6-luna","messages":[{"role":"tool","content":"result"}]}`),
	}
	for i, body := range cases {
		if _, _, err := workBuddyPromptFromBody(body); err == nil {
			t.Fatalf("case %d: expected rejection", i)
		}
	}
}

func TestParseWorkBuddyCLIOutputPreservesCredit(t *testing.T) {
	raw := []byte(`[
		{
			"type":"message",
			"role":"assistant",
			"status":"completed",
			"providerData":{
				"model":"gpt-5.6-luna",
				"rawUsage":{
					"prompt_tokens":13930,
					"completion_tokens":8,
					"total_tokens":13938,
					"prompt_cache_write_tokens":13927,
					"cache_read_input_tokens":0,
					"credit":0.35
				}
			}
		},
		{
			"type":"result",
			"subtype":"success",
			"is_error":false,
			"result":"WB_LEAN_OK",
			"session_id":"session-1"
		}
	]`)
	got, err := parseWorkBuddyCLIOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "WB_LEAN_OK" || got.Model != "gpt-5.6-luna" {
		t.Fatalf("result mismatch: %+v", got)
	}
	if got.PromptTokens != 13930 || got.CompletionTokens != 8 || got.PromptCacheWriteTokens != 13927 {
		t.Fatalf("usage mismatch: %+v", got)
	}
	if got.Credit != 0.35 {
		t.Fatalf("credit=%v want 0.35", got.Credit)
	}
}

func TestRegisterAllIncludesWorkBuddySession(t *testing.T) {
	RegisterAll()
	if Get("workbuddy-session") == nil {
		t.Fatal("workbuddy-session executor is not registered")
	}
}

func TestWorkBuddySessionFailureIsSafeAndRoutable(t *testing.T) {
	const secret = "session-token-private-123"
	tests := []struct {
		name   string
		err    error
		output string
		status int
		code   string
	}{
		{"quota", errors.New("exit status 1"), "quota exceeded; token=" + secret, http.StatusTooManyRequests, "workbuddy_quota"},
		{"expired", errors.New("exit status 1"), "session expired; token=" + secret, http.StatusUnauthorized, "workbuddy_session_expired"},
		{"missing", exec.ErrNotFound, secret, http.StatusServiceUnavailable, "workbuddy_cli_missing"},
		{"unknown", errors.New("exit status 1"), secret, http.StatusBadGateway, "workbuddy_cli_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workBuddySessionFailure(tt.err, tt.output)
			var upstream *proxy.UpstreamError
			if !errors.As(got, &upstream) || upstream.StatusCode != tt.status {
				t.Fatalf("status: got %v, want %d", got, tt.status)
			}
			if !strings.Contains(string(upstream.Body), tt.code) {
				t.Fatalf("body %q missing code %s", upstream.Body, tt.code)
			}
			if strings.Contains(got.Error(), secret) || strings.Contains(string(upstream.Body), secret) {
				t.Fatal("session secret leaked through error")
			}
		})
	}
}

func TestParseWorkBuddyErrorDoesNotReturnCLIText(t *testing.T) {
	const secret = "session-token-private-123"
	_, err := parseWorkBuddyCLIOutput([]byte(`[{"type":"result","is_error":true,"result":"session expired ` + secret + `"}]`))
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe parser error: %v", err)
	}
	var resultErr *workBuddyResultError
	if !errors.As(err, &resultErr) {
		t.Fatalf("missing classified result error: %v", err)
	}
	got := workBuddySessionFailure(err, resultErr.detail).(*proxy.UpstreamError)
	if got.StatusCode != http.StatusUnauthorized || strings.Contains(got.Error(), secret) {
		t.Fatalf("unsafe session failure: %v", got)
	}
}

func TestWorkBuddyQuotaCooldownSkipsCLIAndExpires(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock CLI uses a POSIX shell")
	}
	workBuddySessionQuotaUntil = time.Time{}
	defer func() { workBuddySessionQuotaUntil = time.Time{} }()

	dir := t.TempDir()
	counter := filepath.Join(dir, "invocations")
	cli := filepath.Join(dir, "codebuddy-mock")
	script := "#!/bin/sh\nprintf x >> \"$WORKBUDDY_TEST_COUNTER\"\nprintf 'quota exceeded\\n' >&2\nexit 1\n"
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKBUDDY_CODEBUDDY_PATH", cli)
	t.Setenv("WORKBUDDY_TEST_COUNTER", counter)
	req := &Request{Ctx: context.Background(), Body: []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hi"}]}`)}
	for i, wantCalls := range []int{1, 1, 2} {
		if i == 2 {
			workBuddySessionQuotaUntil = time.Now().Add(-time.Second)
		}
		err := ForwardWorkBuddySession(httptest.NewRecorder(), req)
		var upstream *proxy.UpstreamError
		if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("request %d: expected safe 429, got %v", i, err)
		}
		calls, readErr := os.ReadFile(counter)
		if readErr != nil || len(calls) != wantCalls {
			t.Fatalf("request %d: CLI calls=%d, want %d (read error: %v)", i, len(calls), wantCalls, readErr)
		}
	}
}
