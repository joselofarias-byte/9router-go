package executor

import (
	"strings"
	"testing"
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
