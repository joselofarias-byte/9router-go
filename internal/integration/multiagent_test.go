//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"9router/proxy/internal/auth"
	"9router/proxy/internal/multiagent"
)

func TestMultiagentRequiresAdminEvenWithOpenDashboard(t *testing.T) {
	env := newEnv(t)
	t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS", "deepseek/deepseek-chat")
	t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL", "1")
	if err := env.Repo.UpdateSettingsRaw(map[string]any{"requireLogin": false}); err != nil { t.Fatal(err) }
	upstream := env.NewUpstream(t, chatCompletionResponder())
	env.AddConnection(t, "multiagent-deepseek", "deepseek", "Multiagent", upstream, "sk-upstream")
	body := multiagent.Request{Prompt: "Summarize the Go memory model.", Models: []string{"deepseek/deepseek-chat"}, AllowExternal: true}
	for _, key := range []string{"", env.APIKey, "sk-infer-test"} {
		res := env.Post(t, "/api/multiagent/run", body, WithAPIKey(key))
		if res.Status != http.StatusUnauthorized { t.Fatalf("run with engine key: %d %s", res.Status, truncate(res.Body)) }
		res = env.Get(t, "/api/multiagent/policy", WithAPIKey(key))
		if res.Status != http.StatusUnauthorized { t.Fatalf("policy with engine key: %d", res.Status) }
	}
	if upstream.Count() != 0 { t.Fatal("rejected requests reached the provider") }
}

func TestMultiagentRouterDispatchAndDryRun(t *testing.T) {
	env := newEnv(t)
	t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS", "deepseek/deepseek-chat,deepseek/deepseek-reasoner")
	t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL", "1")
	upstream := env.NewUpstream(t, chatCompletionResponder())
	env.AddConnection(t, "multiagent-deepseek", "deepseek", "Multiagent", upstream, "sk-upstream")
	session, err := auth.Sign(auth.Secret(), time.Now())
	if err != nil { t.Fatal(err) }
	admin := WithHeader("Cookie", auth.CookieName+"="+session)
	res := env.Get(t, "/api/multiagent/policy", WithoutAPIKey(), admin)
	if res.Status != http.StatusOK { t.Fatalf("policy: %d %s", res.Status, truncate(res.Body)) }
	var policy multiagent.Policy
	res.Decode(t, &policy)
	if len(policy.Models) != 2 || !policy.ExternalEnabled { t.Fatalf("policy: %+v", policy) }
	body := multiagent.Request{
		Prompt: "Summarize the Go memory model.",
		Models: []string{"deepseek/deepseek-reasoner", "deepseek/deepseek-chat"},
		Concurrency: 2, AllowExternal: true, DryRun: true,
	}
	res = env.Post(t, "/api/multiagent/run", body, WithoutAPIKey(), admin)
	if res.Status != http.StatusOK || upstream.Count() != 0 { t.Fatalf("dry-run: %d calls=%d", res.Status, upstream.Count()) }
	body.DryRun = false
	res = env.Post(t, "/api/multiagent/run", body, WithoutAPIKey(), admin)
	if res.Status != http.StatusOK { t.Fatalf("dispatch: %d %s", res.Status, truncate(res.Body)) }
	var response multiagent.Response
	res.Decode(t, &response)
	if len(response.Results) != 2 { t.Fatalf("results: %+v", response) }
	for i, result := range response.Results {
		if result.Model != body.Models[i] || result.Error != "" || result.Output == "" { t.Fatalf("result %d: %+v", i, result) }
	}
	if upstream.Count() != 2 { t.Fatalf("provider calls: %d", upstream.Count()) }
}
