package proxy

import (
	"strings"
	"testing"
)

func TestResolveGrokCLIURL(t *testing.T) {
	cases := map[string]string{
		"":                                    "https://cli-chat-proxy.grok.com/v1/responses",
		"https://cli-chat-proxy.grok.com":     "https://cli-chat-proxy.grok.com/v1/responses",
		"https://cli-chat-proxy.grok.com/":    "https://cli-chat-proxy.grok.com/v1/responses",
		"https://cli-chat-proxy.grok.com/v1":  "https://cli-chat-proxy.grok.com/v1/responses",
		"https://custom.example/v1/responses": "https://custom.example/v1/responses",
	}
	for in, want := range cases {
		got := resolveGrokCLIURL(in)
		if got != want && !strings.HasSuffix(got, "/v1/responses") && in == "https://custom.example/v1/responses" {
			t.Fatalf("in=%q got=%q want=%q", in, got, want)
		}
		if got != want {
			t.Fatalf("in=%q got=%q want=%q", in, got, want)
		}
	}
}
