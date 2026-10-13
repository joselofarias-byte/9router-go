package multiagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyFileReloadsWithoutRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("CAPIMUX_MULTIAGENT_POLICY_FILE", path)
	t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL", "1")
	if currentPolicy().ExternalEnabled {
		t.Fatal("missing owner file must disable dispatch")
	}
	for _, body := range []string{
		`{"models":["one/a"],"external_enabled":true}`,
		`{"models":["two/b"],"external_enabled":false}`,
	} {
		if err := os.WriteFile(path+".new", []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".new", path); err != nil {
			t.Fatal(err)
		}
		p := currentPolicy()
		if len(p.Models) != 1 || !strings.Contains(body, p.Models[0]) || p.ExternalEnabled != strings.Contains(body, "true") {
			t.Fatalf("stale policy: %+v", p)
		}
		if p.MaxModels != 8 || p.MaxOutputTokens != 4096 {
			t.Fatal("file changed engine limits")
		}
	}
}

func TestInvalidPolicyFilesDisableDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("CAPIMUX_MULTIAGENT_POLICY_FILE", path)
	t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL", "1")
	t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS", "fallback/model")
	for _, body := range []string{
		`{"models":["one/a"],"external_enabled":true} trailing`,
		`{"models":[""],"external_enabled":true}`,
		strings.Repeat(" ", 65537),
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		p := currentPolicy()
		if p.ExternalEnabled || len(p.Models) != 0 {
			t.Fatalf("invalid policy enabled dispatch: %+v", p)
		}
	}
}
