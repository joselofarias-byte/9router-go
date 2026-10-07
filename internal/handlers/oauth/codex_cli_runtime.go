package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const codexCLIAuthMaxBytes = 1 << 20

var (
	codexAuthURLPattern = regexp.MustCompile(`https://auth\.openai\.com/[^\s\x1b]+`)
	codexANSISequence   = regexp.MustCompile("\\x1b\\[[0-9;?]*[ -/]*[@-~]")
	codexCLILogins      = newCodexCLILoginManager()
)

type codexCLILoginSession struct {
	status       string
	connectionID string
	email        string
	err          string
	name         string
	cancel       context.CancelFunc
}

type codexCLILoginManager struct {
	mu       sync.Mutex
	sessions map[string]*codexCLILoginSession
}

func newCodexCLILoginManager() *codexCLILoginManager {
	return &codexCLILoginManager{sessions: make(map[string]*codexCLILoginSession)}
}

func (m *codexCLILoginManager) register(id, name string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &codexCLILoginSession{status: "pending", name: name, cancel: cancel}
}

func (m *codexCLILoginManager) complete(id, connectionID, email string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess := m.sessions[id]; sess != nil {
		sess.status = "done"
		sess.connectionID = connectionID
		sess.email = email
		sess.cancel = nil
	}
}

func (m *codexCLILoginManager) fail(id, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess := m.sessions[id]; sess != nil {
		sess.status = "error"
		sess.err = message
		sess.cancel = nil
	}
}

func (m *codexCLILoginManager) name(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess := m.sessions[id]; sess != nil {
		return sess.name
	}
	return ""
}

func (m *codexCLILoginManager) take(id string) (codexCLILoginSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.sessions[id]
	if sess == nil {
		return codexCLILoginSession{}, false
	}
	copy := *sess
	if copy.status != "pending" {
		delete(m.sessions, id)
	}
	return copy, true
}

func (m *codexCLILoginManager) cancel(id string) bool {
	m.mu.Lock()
	sess := m.sessions[id]
	if sess != nil {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if sess == nil {
		return false
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	return true
}

func prepareCodexLoginHome() (string, error) {
	home, err := os.MkdirTemp("", "9router-codex-login-*")
	if err != nil {
		return "", fmt.Errorf("make Codex login home: %w", err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		_ = os.RemoveAll(home)
		return "", fmt.Errorf("secure Codex login home: %w", err)
	}
	return home, nil
}

func extractCodexAuthURL(line string) string {
	clean := codexANSISequence.ReplaceAllString(line, "")
	return codexAuthURLPattern.FindString(clean)
}

func readCodexCLIAuth(home string) (*pkceTokens, error) {
	path := filepath.Join(home, "auth.json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat Codex auth: %w", err)
	}
	if info.Size() > codexCLIAuthMaxBytes {
		return nil, fmt.Errorf("Codex auth file too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Codex auth: %w", err)
	}
	var doc struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			IDToken      string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode Codex auth: %w", err)
	}
	if strings.TrimSpace(doc.Tokens.AccessToken) == "" {
		return nil, fmt.Errorf("Codex auth has no access token")
	}
	now := time.Now()
	expiresIn := jwtExpiresIn(doc.Tokens.AccessToken, now)
	if expiresIn == 0 {
		expiresIn = jwtExpiresIn(doc.Tokens.IDToken, now)
	}
	return &pkceTokens{
		AccessToken:  doc.Tokens.AccessToken,
		RefreshToken: doc.Tokens.RefreshToken,
		IDToken:      doc.Tokens.IDToken,
		ExpiresIn:    expiresIn,
	}, nil
}

func jwtExpiresIn(token string, now time.Time) int {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= now.Unix() {
		return 0
	}
	seconds := claims.Exp - now.Unix()
	if seconds > int64(^uint(0)>>1) {
		return 0
	}
	return int(seconds)
}

func codexLoginEnv(home string) []string {
	blocked := map[string]bool{
		"CODEX_HOME":         true,
		"OPENAI_API_KEY":     true,
		"CODEX_API_KEY":      true,
		"CODEX_ACCESS_TOKEN": true,
	}
	base := os.Environ()
	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && blocked[strings.ToUpper(key)] {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "CODEX_HOME="+home, "NO_COLOR=1")
}

func findCodexLoginBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("CODEX_LOGIN_BIN")); explicit != "" {
		if isExecutableFile(explicit) {
			return explicit, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		native := filepath.Join(home, ".local", "codex-termux", "node_modules", ".bin", "codex")
		if isExecutableFile(native) {
			return native, nil
		}
	}
	for _, name := range []string{"codex-termux", "codex"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Codex CLI not found")
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}
