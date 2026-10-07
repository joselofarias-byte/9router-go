package oauth

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"9router/proxy/internal/handlerutil"
)

const (
	codexCLIStartTimeout = 20 * time.Second
	codexCLILoginTimeout = 6 * time.Minute
	codexCLIAuthMaxBytes = 1 << 20
)

var (
	codexAuthURLPattern = regexp.MustCompile(`https://auth\\.openai\\.com/[^\\s\\x1b]+`)
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

// HandleCodexCLIStart delegates ChatGPT OAuth to the installed official Codex
// CLI, mirroring AnyClaw: Codex owns PKCE, the loopback callback, token
// exchange and auth.json. 9router only captures the authorization URL and
// imports the resulting credentials from an isolated CODEX_HOME.
//
// POST /api/oauth/codex/cli-login/start
func (h *OAuthHandler) HandleCodexCLIStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name,omitempty"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	}

	bin, err := findCodexLoginBinary()
	if err != nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"reason":  "codex_not_found",
		})
		return
	}

	// The official CLI needs its own fixed loopback callback. Retire a manual
	// Codex proxy left by an earlier dashboard attempt before spawning it.
	codexLoopback.stop()

	home, err := os.MkdirTemp("", "9router-codex-login-*")
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to prepare isolated Codex login")
		return
	}
	if err := os.Chmod(home, 0o700); err != nil {
		_ = os.RemoveAll(home)
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to secure isolated Codex login")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), codexCLILoginTimeout)
	cmd := exec.CommandContext(ctx, bin, "login")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home, "NO_COLOR=1")

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		cancel()
		_ = reader.Close()
		_ = writer.Close()
		_ = os.RemoveAll(home)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"reason":  "start_failed",
		})
		return
	}

	sessionID := randomString(24)
	codexCLILogins.register(sessionID, strings.TrimSpace(body.Name), cancel)

	urlCh := make(chan string, 1)
	go scanCodexLoginOutput(reader, urlCh)
	exitCh := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		_ = writer.Close()
		exitCh <- waitErr
		h.finishCodexCLILogin(sessionID, home, ctx.Err(), waitErr)
	}()

	timer := time.NewTimer(codexCLIStartTimeout)
	defer timer.Stop()
	select {
	case authURL := <-urlCh:
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success":   true,
			"sessionId": sessionID,
			"authUrl":   authURL,
			"source":    "codex_cli",
		})
	case <-exitCh:
		codexCLILogins.cancel(sessionID)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"reason":  "login_exited",
		})
	case <-timer.C:
		codexCLILogins.cancel(sessionID)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"reason":  "url_timeout",
		})
	}
}

func scanCodexLoginOutput(r io.Reader, urlCh chan<- string) {
	defer func() { _ = recover() }()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	sent := false
	for scanner.Scan() {
		if sent {
			continue
		}
		if authURL := extractCodexAuthURL(scanner.Text()); authURL != "" {
			urlCh <- authURL
			sent = true
		}
	}
}

func extractCodexAuthURL(line string) string {
	clean := codexANSISequence.ReplaceAllString(line, "")
	return codexAuthURLPattern.FindString(clean)
}

func (h *OAuthHandler) finishCodexCLILogin(sessionID, home string, ctxErr, waitErr error) {
	defer os.RemoveAll(home)
	if ctxErr != nil {
		codexCLILogins.fail(sessionID, "Codex login timed out or was canceled")
		return
	}
	if waitErr != nil {
		codexCLILogins.fail(sessionID, "Codex login did not complete")
		return
	}

	tokens, err := readCodexCLIAuth(home)
	if err != nil {
		codexCLILogins.fail(sessionID, "Codex login completed but its credentials could not be imported")
		return
	}

	ex := &pkceExchange{
		cfg:      pkceProviders["codex"],
		provider: "codex",
		name:     codexCLILogins.name(sessionID),
	}
	dataMap, email := ex.buildConnectionData(context.Background(), tokens)
	connName := connectionDisplayName("codex", ex.name, email, ex.cfg.display)
	connID := ex.cfg.connPrefix + shortHash(tokens.AccessToken)
	if err := h.persistConnection(connID, "codex", connName, dataMap, tokens.RefreshToken); err != nil {
		codexCLILogins.fail(sessionID, "Codex login succeeded but the account could not be saved")
		return
	}
	codexCLILogins.complete(sessionID, connID, email)
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
	return &pkceTokens{
		AccessToken:  doc.Tokens.AccessToken,
		RefreshToken: doc.Tokens.RefreshToken,
		IDToken:      doc.Tokens.IDToken,
		ExpiresIn:    jwtExpiresIn(doc.Tokens.AccessToken, time.Now()),
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

// GET /api/oauth/codex/cli-login/status?id=
func (h *OAuthHandler) HandleCodexCLIStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "missing id")
		return
	}
	sess, ok := codexCLILogins.take(id)
	if !ok {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"status": "unknown"})
		return
	}
	payload := map[string]any{"status": sess.status}
	if sess.connectionID != "" {
		payload["connectionId"] = sess.connectionID
	}
	if sess.email != "" {
		payload["email"] = sess.email
	}
	if sess.err != "" {
		payload["error"] = sess.err
	}
	handlerutil.WriteJSON(w, http.StatusOK, payload)
}

// POST /api/oauth/codex/cli-login/cancel?id=
func (h *OAuthHandler) HandleCodexCLICancel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": id != "" && codexCLILogins.cancel(id),
	})
}
