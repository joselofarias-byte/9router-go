package chat

import (
	"context"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/promptprofile"
)

func TestWithRequestPromptProfile(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set(promptprofile.Header, promptprofile.OpenAIAgenticV1)
	ctx, err := withRequestPromptProfile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := promptprofile.FromContext(ctx); got != promptprofile.OpenAIAgenticV1 {
		t.Fatalf("profile=%q", got)
	}
}

func TestWithRequestPromptProfileRejectsUnknown(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set(promptprofile.Header, "does-not-exist")
	if _, err := withRequestPromptProfile(context.Background(), req); err == nil {
		t.Fatal("expected unknown profile error")
	}
}
