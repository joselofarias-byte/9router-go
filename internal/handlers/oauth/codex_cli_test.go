package oauth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractCodexAuthURL(t *testing.T) {
	line := "\x1b[32mOpen this URL: https://auth.openai.com/oauth/authorize?client_id=test&state=abc\x1b[0m"
	got := extractCodexAuthURL(line)
	want := "https://auth.openai.com/oauth/authorize?client_id=test&state=abc"
	if got != want {
		t.Fatalf("extractCodexAuthURL() = %q, want %q", got, want)
	}
}

func TestReadCodexCLIAuth(t *testing.T) {
	dir := t.TempDir()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2000000000}`))
	access := "eyJ." + payload + ".sig"
	raw := `{"auth_mode":"chatgpt","tokens":{"access_token":"` + access + `","refresh_token":"refresh-value","id_token":"id-value"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	tokens, err := readCodexCLIAuth(dir)
	if err != nil {
		t.Fatalf("readCodexCLIAuth() error = %v", err)
	}
	if tokens.AccessToken != access || tokens.RefreshToken != "refresh-value" || tokens.IDToken != "id-value" {
		t.Fatalf("unexpected imported token fields")
	}
	if tokens.ExpiresIn <= 0 {
		t.Fatalf("ExpiresIn = %d, want positive", tokens.ExpiresIn)
	}
}

func TestJWTExpiresInRejectsExpiredAndMalformed(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	cases := []string{
		"not-a-jwt",
		"eyJ.bad.sig",
		"eyJ." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1800000000}`)) + ".sig",
	}
	for _, token := range cases {
		if got := jwtExpiresIn(token, now); got != 0 {
			t.Errorf("jwtExpiresIn(%q) = %d, want 0", token, got)
		}
	}
}
