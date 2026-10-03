package dashboard

import (
	"bytes"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"9router/proxy/internal/auth"
)

func clearGoogleBackupStatesForTest() {
	googleDriveBackupStates.Range(func(key, value any) bool {
		googleDriveBackupStates.Delete(key)
		return true
	})
}

func TestHandleGoogleBackupAuthorizeUsesNarrowDriveScope(t *testing.T) {
	repo, cleanup := setupSettingsTestDB(t)
	defer cleanup()
	router := setupTestRouter(repo)

	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_ID", "CLIENT_ID_VALUE")
	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET", "CLIENT_SECRET_VALUE")
	clearGoogleBackupStatesForTest()
	t.Cleanup(clearGoogleBackupStatesForTest)

	oldAuthURL := googleDriveBackupAuthURL
	googleDriveBackupAuthURL = "https://accounts.example.test/o/oauth2/v2/auth"
	t.Cleanup(func() { googleDriveBackupAuthURL = oldAuthURL })

	redirectURI := "http://localhost:20130/callback"
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/settings/backup/google/authorize?redirect_uri="+url.QueryEscape(redirectURI),
		nil,
	)
	req.Header.Set(cliTokenHeader, auth.CLIToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		URL         string `json:"url"`
		State       string `json:"state"`
		RedirectURI string `json:"redirectUri"`
		Scope       string `json:"scope"`
		Persistent  bool   `json:"persistent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.State == "" {
		t.Fatal("authorize response missing state")
	}
	if body.RedirectURI != redirectURI {
		t.Fatalf("redirect=%q want=%q", body.RedirectURI, redirectURI)
	}
	if body.Scope != googleDriveBackupScope {
		t.Fatalf("scope=%q want=%q", body.Scope, googleDriveBackupScope)
	}
	if body.Persistent {
		t.Fatal("one-shot Google OAuth must not claim persistent credentials")
	}
	u, err := url.Parse(body.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("scope") != googleDriveBackupScope {
		t.Fatalf("oauth URL scope=%q", q.Get("scope"))
	}
	if q.Get("scope") == "https://www.googleapis.com/auth/drive" {
		t.Fatal("oauth URL requested full Drive scope")
	}
	if q.Get("access_type") != "online" {
		t.Fatalf("access_type=%q want online", q.Get("access_type"))
	}
}

func TestHandleGoogleBackupUploadEncryptsBeforeDriveAndRotates(t *testing.T) {
	repo, cleanup := setupSettingsTestDB(t)
	defer cleanup()
	router := setupTestRouter(repo)

	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_ID", "CLIENT_ID_VALUE")
	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET", "CLIENT_SECRET_VALUE")
	clearGoogleBackupStatesForTest()
	t.Cleanup(clearGoogleBackupStatesForTest)

	const privateMarker = "PRIVATE_PROVIDER_VALUE"
	if _, err := repo.RawDB().Exec(
		`INSERT INTO providerConnections (id, provider, authType, name, isActive, priority, data, createdAt, updatedAt)
		 VALUES ('conn-google-backup', 'antigravity', 'oauth', 'AG', 1, 1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`{"privateMarker":"`+privateMarker+`"}`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	var (
		tokenHits atomic.Int32
		mu        sync.Mutex
		uploaded  []byte
		deleted   []string
	)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token" && r.Method == http.MethodPost:
			tokenHits.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.Form.Get("code") != "AUTH_CODE_VALUE" {
				t.Errorf("unexpected authorization code")
			}
			if r.Form.Get("client_id") != "CLIENT_ID_VALUE" ||
				r.Form.Get("client_secret") != "CLIENT_SECRET_VALUE" {
				t.Errorf("unexpected OAuth client credentials")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"ACCESS_VALUE","token_type":"Bearer","expires_in":3600}`)

		case r.URL.Path == "/drive/v3/files" && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer ACCESS_VALUE" {
				t.Errorf("unexpected Drive authorization header")
			}
			query := r.URL.Query().Get("q")
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(query, "application/vnd.google-apps.folder") {
				_, _ = io.WriteString(w, `{"files":[]}`)
				return
			}
			if strings.Contains(query, "'folder-1' in parents") {
				_, _ = io.WriteString(w, `{"files":[
					{"id":"file-new","name":"9router-backup-new.9rbak","createdTime":"2026-10-03T19:00:00Z","appProperties":{"kind":"9router-encrypted-backup"}},
					{"id":"old-1","name":"9router-backup-1.9rbak","createdTime":"2026-10-03T18:00:00Z","appProperties":{"kind":"9router-encrypted-backup"}},
					{"id":"old-2","name":"9router-backup-2.9rbak","createdTime":"2026-10-03T17:00:00Z","appProperties":{"kind":"9router-encrypted-backup"}},
					{"id":"old-3","name":"9router-backup-3.9rbak","createdTime":"2026-10-03T16:00:00Z","appProperties":{"kind":"9router-encrypted-backup"}},
					{"id":"old-4","name":"9router-backup-4.9rbak","createdTime":"2026-10-03T15:00:00Z","appProperties":{"kind":"9router-encrypted-backup"}},
					{"id":"old-5","name":"9router-backup-5.9rbak","createdTime":"2026-10-03T14:00:00Z","appProperties":{"kind":"9router-encrypted-backup"}},
					{"id":"other","name":"notes.txt","createdTime":"2026-10-03T13:00:00Z","appProperties":{}}
				]}`)
				return
			}
			t.Errorf("unexpected Drive list query")
			http.Error(w, "unexpected list", http.StatusBadRequest)

		case r.URL.Path == "/drive/v3/files" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if !bytes.Contains(raw, []byte(googleDriveBackupFolderName)) {
				t.Errorf("backup folder metadata missing")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"folder-1","name":"9router Backups"}`)

		case r.URL.Path == "/upload/drive/v3/files" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			mu.Lock()
			uploaded = append([]byte(nil), raw...)
			mu.Unlock()
			if bytes.Contains(raw, []byte(privateMarker)) {
				t.Error("Drive upload contained plaintext provider data")
			}
			if !bytes.Contains(raw, []byte(backupEncryptedMagic)) {
				t.Error("Drive upload did not contain encrypted 9RBKENC1 payload")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"file-new","name":"9router-backup-new.9rbak","createdTime":"2026-10-03T19:00:00Z","size":"2048","appProperties":{"kind":"9router-encrypted-backup"}}`)

		case strings.HasPrefix(r.URL.Path, "/drive/v3/files/") && r.Method == http.MethodDelete:
			mu.Lock()
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/drive/v3/files/"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer fake.Close()

	oldAuthURL := googleDriveBackupAuthURL
	oldTokenURL := googleDriveBackupTokenURL
	oldAPIURL := googleDriveBackupAPIURL
	oldUploadURL := googleDriveBackupUploadURL
	oldClient := googleDriveBackupClient
	googleDriveBackupAuthURL = fake.URL + "/auth"
	googleDriveBackupTokenURL = fake.URL + "/token"
	googleDriveBackupAPIURL = fake.URL + "/drive/v3"
	googleDriveBackupUploadURL = fake.URL + "/upload/drive/v3"
	googleDriveBackupClient = fake.Client()
	t.Cleanup(func() {
		googleDriveBackupAuthURL = oldAuthURL
		googleDriveBackupTokenURL = oldTokenURL
		googleDriveBackupAPIURL = oldAPIURL
		googleDriveBackupUploadURL = oldUploadURL
		googleDriveBackupClient = oldClient
	})

	redirectURI := "http://localhost:20130/callback"
	authReq := httptest.NewRequest(
		http.MethodGet,
		"/api/settings/backup/google/authorize?redirect_uri="+url.QueryEscape(redirectURI),
		nil,
	)
	authReq.Header.Set(cliTokenHeader, auth.CLIToken())
	authRec := httptest.NewRecorder()
	router.ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", authRec.Code, authRec.Body.String())
	}
	var authBody struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(authRec.Body.Bytes(), &authBody); err != nil || authBody.State == "" {
		t.Fatalf("authorize response=%s err=%v", authRec.Body.String(), err)
	}

	requestBody, _ := json.Marshal(map[string]string{
		"code":        "AUTH_CODE_VALUE",
		"redirectUri": redirectURI,
		"state":       authBody.State,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/google", bytes.NewReader(requestBody))
	req.Header.Set(cliTokenHeader, auth.CLIToken())
	req.Header.Set(backupPassphraseHeader, "google backup recovery phrase")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backup status=%d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Success        bool `json:"success"`
		RotatedDeleted int  `json:"rotatedDeleted"`
		OAuthPersisted bool `json:"oauthPersisted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.RotatedDeleted != 1 || response.OAuthPersisted {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if tokenHits.Load() != 1 {
		t.Fatalf("token exchange hits=%d want=1", tokenHits.Load())
	}

	mu.Lock()
	uploadedLen := len(uploaded)
	deletedCopy := append([]string(nil), deleted...)
	mu.Unlock()
	if uploadedLen == 0 {
		t.Fatal("no encrypted backup reached fake Drive")
	}
	if len(deletedCopy) != 1 || deletedCopy[0] != "old-5" {
		t.Fatalf("rotation deleted=%v want [old-5]", deletedCopy)
	}
}

func TestHandleGoogleBackupAuthorizeRequiresDedicatedOAuthConfig(t *testing.T) {
	repo, cleanup := setupSettingsTestDB(t)
	defer cleanup()
	router := setupTestRouter(repo)

	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_ID", "")
	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET", "")

	req := httptest.NewRequest(http.MethodGet, "/api/settings/backup/google/authorize", nil)
	req.Header.Set(cliTokenHeader, auth.CLIToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=503 body=%s", rec.Code, rec.Body.String())
	}
}

func TestValidateGoogleBackupRedirectURIRejectsPlainHTTPRemoteHost(t *testing.T) {
	if err := validateGoogleBackupRedirectURI("http://example.com/callback"); err == nil {
		t.Fatal("plain HTTP remote redirect was accepted")
	}
	if err := validateGoogleBackupRedirectURI("http://localhost:20130/callback"); err != nil {
		t.Fatalf("localhost redirect rejected: %v", err)
	}
	if err := validateGoogleBackupRedirectURI("https://example.com/callback"); err != nil {
		t.Fatalf("https redirect rejected: %v", err)
	}
}
