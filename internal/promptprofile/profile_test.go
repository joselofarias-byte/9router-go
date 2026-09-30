package promptprofile

import (
	"context"
	"strings"
	json "encoding/json/v2"
	"testing"
)

func TestLookupAndProviderScope(t *testing.T) {
	p, ok := Lookup(OpenAIAgenticV1)
	if !ok || p.Name != OpenAIAgenticV1 || p.Instructions == "" {
		t.Fatalf("profile lookup failed: ok=%v profile=%+v", ok, p)
	}
	if !p.Supports("openai") || !p.Supports("CODEX") {
		t.Fatal("profile must support openai and codex")
	}
	if p.Supports("anthropic") {
		t.Fatal("profile must not apply to unrelated providers")
	}
	if _, ok := Lookup("unknown"); ok {
		t.Fatal("unknown profile must be rejected")
	}
	if p, ok := Lookup("none"); !ok || p.Name != "" {
		t.Fatal("none must be an accepted opt-out")
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithName(context.Background(), OpenAIAgenticV1)
	if got := FromContext(ctx); got != OpenAIAgenticV1 {
		t.Fatalf("context profile=%q", got)
	}
}

func TestInjectResponsesInstructions(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":[{"role":"user","content":"hello"}],"instructions":"original"}`)
	out, changed, err := Inject(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("inject failed: changed=%v err=%v", changed, err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["instructions"] != "original\n\nagentic" {
		t.Fatalf("instructions=%q", got["instructions"])
	}
}

func TestInjectChatUsesDeveloperRole(t *testing.T) {
	body := []byte(`{"model":"gpt-test","messages":[{"role":"system","content":"system"},{"role":"user","content":"hello"}]}`)
	out, changed, err := Inject(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("inject failed: changed=%v err=%v", changed, err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages=%d", len(msgs))
	}
	dev := msgs[1].(map[string]any)
	if dev["role"] != "developer" || dev["content"] != "agentic" {
		t.Fatalf("developer message=%v", dev)
	}
}

func TestInjectExtendsExistingDeveloperAndIsIdempotent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"developer","content":"original"},{"role":"user","content":"hello"}]}`)
	out, changed, err := Inject(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("first inject failed: changed=%v err=%v", changed, err)
	}
	out2, changed2, err := Inject(out, "agentic")
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Fatal("second inject must be idempotent")
	}
	if string(out2) != string(out) {
		t.Fatal("idempotent inject changed body")
	}
}

func TestInjectCodexPreservesFirstSystemInstruction(t *testing.T) {
	body := []byte(`{"model":"gpt-test","messages":[{"role":"system","content":"caller system"},{"role":"user","content":"hello"}]}`)
	out, changed, err := InjectCodex(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("inject failed: changed=%v err=%v", changed, err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	msgs := got["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "caller system\n\nagentic" {
		t.Fatalf("first instruction=%v", first)
	}
}

func TestAllBuiltInProfilesAreRegisteredAndScoped(t *testing.T) {
	names := []string{
		OpenAIAgenticV1,
		WorkspaceContextV1,
		FewshotRoutingV1,
		OperatingSpecV1,
	}
	for _, name := range names {
		p, ok := Lookup(name)
		if !ok {
			t.Fatalf("profile %q not registered", name)
		}
		if p.Name != name || strings.TrimSpace(p.Instructions) == "" {
			t.Fatalf("profile %q malformed: %+v", name, p)
		}
		if !p.Supports("openai") || !p.Supports("codex") {
			t.Fatalf("profile %q must support openai and codex", name)
		}
		if p.Supports("anthropic") {
			t.Fatalf("profile %q unexpectedly supports anthropic", name)
		}
	}
}

func TestNamesStableAndComplete(t *testing.T) {
	want := []string{
		"none",
		OpenAIAgenticV1,
		WorkspaceContextV1,
		FewshotRoutingV1,
		OperatingSpecV1,
	}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("Names len=%d want=%d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names[%d]=%q want=%q", i, got[i], want[i])
		}
	}
}
