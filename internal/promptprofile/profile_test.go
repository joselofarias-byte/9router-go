package promptprofile

import (
	"context"
	json "encoding/json/v2"
	"strings"
	"testing"
)

func TestLookupAndProviderScope(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
		want   string
		openai bool
		codex  bool
		other  bool
	}{
		{name: "canonical agentic profile", input: OpenAIAgenticV1, wantOK: true, want: OpenAIAgenticV1, openai: true, codex: true},
		{name: "case and space insensitive", input: "  OpenAI-Agentic-V1 ", wantOK: true, want: OpenAIAgenticV1, openai: true, codex: true},
		{name: "unknown name rejected", input: "unknown", wantOK: false},
		{name: "none is explicit opt-out", input: "none", wantOK: true, want: ""},
		{name: "empty is accepted opt-out", input: "  ", wantOK: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Lookup(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Name != tt.want {
				t.Fatalf("name=%q, want %q", got.Name, tt.want)
			}
			if tt.want == "" {
				return
			}
			if strings.TrimSpace(got.Instructions) == "" {
				t.Fatal("instructions empty")
			}
			if got.Supports("openai") != tt.openai || got.Supports("CODEX") != tt.codex {
				t.Fatalf("scope openai=%v codex=%v", got.Supports("openai"), got.Supports("CODEX"))
			}
			if got.Supports("anthropic") != tt.other {
				t.Fatal("profile applied to an unrelated provider")
			}
		})
	}
}

func TestAllBuiltInProfilesAreRegisteredAndScoped(t *testing.T) {
	for _, name := range []string{OpenAIAgenticV1, WorkspaceContextV1, FewshotRoutingV1, OperatingSpecV1} {
		t.Run(name, func(t *testing.T) {
			p, ok := Lookup(name)
			if !ok {
				t.Fatalf("profile %q not registered", name)
			}
			if p.Name != name || strings.TrimSpace(p.Instructions) == "" {
				t.Fatalf("profile malformed: %+v", p)
			}
			if !p.Supports("openai") || !p.Supports("codex") {
				t.Fatal("profile must support openai and codex")
			}
			if p.Supports("anthropic") {
				t.Fatal("profile unexpectedly supports anthropic")
			}
		})
	}
}

func TestNamesStableAndComplete(t *testing.T) {
	want := []string{"none", OpenAIAgenticV1, WorkspaceContextV1, FewshotRoutingV1, OperatingSpecV1}
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

func TestContextRoundTrip(t *testing.T) {
	ctx := WithName(context.Background(), OpenAIAgenticV1)
	if got := FromContext(ctx); got != OpenAIAgenticV1 {
		t.Fatalf("context profile=%q", got)
	}
	if got := FromContext(WithName(context.Background(), "none")); got != "" {
		t.Fatalf("opt-out stored %q", got)
	}
	if got := FromContext(WithName(context.Background(), "unknown")); got != "" {
		t.Fatalf("unknown stored %q", got)
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
	if got["model"] != "gpt-test" {
		t.Fatalf("model lost: %#v", got["model"])
	}
}

func TestInjectChatUsesDeveloperRole(t *testing.T) {
	body := []byte(`{"model":"gpt-test","messages":[{"role":"system","content":"system"},{"role":"user","content":"hello"}]}`)
	out, changed, err := Inject(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("inject failed: changed=%v err=%v", changed, err)
	}
	msgs := messageList(t, out)
	if len(msgs) != 3 {
		t.Fatalf("messages=%d", len(msgs))
	}
	dev := msgs[1].(map[string]any)
	if dev["role"] != "developer" || dev["content"] != "agentic" {
		t.Fatalf("developer message=%v", dev)
	}
	if msgs[0].(map[string]any)["content"] != "system" {
		t.Fatal("caller system instruction was replaced")
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
	dev := messageList(t, out)[0].(map[string]any)
	if dev["content"] != "original\n\nagentic" {
		t.Fatalf("extended content=%v", dev["content"])
	}
}

func TestInjectCodexPreservesFirstSystemInstruction(t *testing.T) {
	body := []byte(`{"model":"gpt-test","messages":[{"role":"system","content":"caller system"},{"role":"user","content":"hello"}]}`)
	out, changed, err := InjectCodex(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("inject failed: changed=%v err=%v", changed, err)
	}
	msgs := messageList(t, out)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "caller system\n\nagentic" {
		t.Fatalf("first instruction=%v", first)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages=%d, want the original two", len(msgs))
	}
}

func TestInjectCodexResponsesExtendsInstructions(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"hello","instructions":"caller"}`)
	out, changed, err := InjectCodex(body, "agentic")
	if err != nil || !changed {
		t.Fatalf("inject failed: changed=%v err=%v", changed, err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["instructions"] != "caller\n\nagentic" {
		t.Fatalf("instructions=%q", got["instructions"])
	}
}

func messageList(t *testing.T, body []byte) []any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	msgs, ok := got["messages"].([]any)
	if !ok {
		t.Fatalf("messages missing: %#v", got["messages"])
	}
	return msgs
}
