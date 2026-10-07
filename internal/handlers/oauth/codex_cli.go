package oauth

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"9router/proxy/internal/handlerutil"
)

const (
	codexCLIStartTimeout = 20 * time.Second
	codexCLILoginTimeout = 6 * time.Minute
)

// HandleCodexCLIStart delegates ChatGPT OAuth to the installed Codex CLI,
// mirroring AnyClaw: Codex owns PKCE, the loopback callback, token exchange and
// auth.json. 9router captures only the authorization URL and imports the
// resulting credentials from an isolated CODEX_HOME.
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

	home, err := prepareCodexLoginHome()
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to prepare isolated Codex login")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), codexCLILoginTimeout)
	cmd := exec.CommandContext(ctx, bin, "-c", `cli_auth_credentials_store="file"`, "login")
	cmd.Env = codexLoginEnv(home)

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
