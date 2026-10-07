package dashboard

import (
	"bytes"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"9router/proxy/internal/auth"
)

func TestGoogleDriveConnectPersistsRefreshTokenButNeverExposesIt(t *testing.T) {
	repo, cleanup := setupSettingsTestDB(t)
	defer cleanup()
	router := setupTestRouter(repo)

	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_ID", "CLIENT_ID_VALUE")
	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET", "CLIENT_SECRET_VALUE")
	clearGoogleBackupStatesForTest()
	t.Cleanup(clearGoogleBackupStatesForTest)

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type=%q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"ACCESS_VALUE","refresh_token":"REFRESH_SECRET_VALUE","token_type":"Bearer","expires_in":3600}`)
	}))
	defer fake.Close()

	oldTokenURL := googleDriveBackupTokenURL
	oldClient := googleDriveBackupClient
	googleDriveBackupTokenURL = fake.URL + "/token"
	googleDriveBackupClient = fake.Client()
	t.Cleanup(func() {
		googleDriveBackupTokenURL = oldTokenURL
		googleDriveBackupClient = oldClient
	})

	redirectURI := "http://localhost:20130/callback"
	authReq := httptest.NewRequest(http.MethodGet,
		"/api/settings/backup/google/authorize?redirect_uri="+url.QueryEscape(redirectURI), nil)
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

	body, _ := json.Marshal(map[string]string{
		"code":        "AUTH_CODE_VALUE",
		"redirectUri": redirectURI,
		"state":       authBody.State,
	})
	connectReq := httptest.NewRequest(http.MethodPost, "/api/settings/backup/google/connect", bytes.NewReader(body))
	connectReq.Header.Set(cliTokenHeader, auth.CLIToken())
	connectReq.Header.Set("Content-Type", "application/json")
	connectRec := httptest.NewRecorder()
	router.ServeHTTP(connectRec, connectReq)
	if connectRec.Code != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", connectRec.Code, connectRec.Body.String())
	}

	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if got := raw[googleDriveBackupRefreshTokenSetting]; got != "REFRESH_SECRET_VALUE" {
		t.Fatalf("stored refresh token=%v", got)
	}

	settingsReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	settingsRec := httptest.NewRecorder()
	router.ServeHTTP(settingsRec, settingsReq)
	if strings.Contains(settingsRec.Body.String(), "REFRESH_SECRET_VALUE") ||
		strings.Contains(settingsRec.Body.String(), googleDriveBackupRefreshTokenSetting) {
		t.Fatalf("GET /api/settings leaked Drive refresh token: %s", settingsRec.Body.String())
	}
}

func TestGoogleDriveListAndRestoreUseStoredRefreshToken(t *testing.T) {
	repo, cleanup := setupSettingsTestDB(t)
	defer cleanup()
	router := setupTestRouter(repo)

	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_ID", "CLIENT_ID_VALUE")
	t.Setenv("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET", "CLIENT_SECRET_VALUE")
	if err := repo.UpdateSettingsRaw(map[string]any{
		googleDriveBackupRefreshTokenSetting: "REFRESH_VALUE",
		"language": "en",
	}); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"settings": map[string]any{"language": "es"},
		"providerConnections": []any{},
		"providerNodes": []any{},
		"proxyPools": []any{},
		"apiKeys": []any{},
		"combos": []any{},
		"modelAliases": map[string]any{},
		"customModels": []any{},
		"mitmAlias": map[string]any{},
		"pricing": map[string]any{},
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := encryptBackup(plain, "restore passphrase 2026")
	if err != nil {
		t.Fatal(err)
	}

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse refresh form: %v", err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "REFRESH_VALUE" {
				t.Errorf("unexpected refresh grant: %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"ACCESS_VALUE","token_type":"Bearer","expires_in":3600}`)

		case r.URL.Path == "/drive/v3/files" && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer ACCESS_VALUE" {
				t.Errorf("missing bearer token")
			}
			q := r.URL.Query().Get("q")
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(q, "application/vnd.google-apps.folder") {
				_, _ = io.WriteString(w, `{"files":[{"id":"folder-1","name":"9router Backups"}]}`)
				return
			}
			if strings.Contains(q, "'folder-1' in parents") {
				_, _ = io.WriteString(w, `{"files":[{"id":"file-1","name":"9router-backup-test.9rbak","createdTime":"2026-10-07T20:00:00Z","size":"2048","appProperties":{"kind":"9router-encrypted-backup"}}]}`)
				return
			}
			http.Error(w, "unexpected query", http.StatusBadRequest)

		case r.URL.Path == "/drive/v3/files/file-1" && r.Method == http.MethodGet && r.URL.Query().Get("alt") == "media":
			w.Header().Set("Content-Type", backupContentType)
			_, _ = w.Write(encrypted)

		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer fake.Close()

	oldTokenURL := googleDriveBackupTokenURL
	oldAPIURL := googleDriveBackupAPIURL
	oldClient := googleDriveBackupClient
	googleDriveBackupTokenURL = fake.URL + "/token"
	googleDriveBackupAPIURL = fake.URL + "/drive/v3"
	googleDriveBackupClient = fake.Client()
	t.Cleanup(func() {
		googleDriveBackupTokenURL = oldTokenURL
		googleDriveBackupAPIURL = oldAPIURL
		googleDriveBackupClient = oldClient
	})

	listReq := httptest.NewRequest(http.MethodGet, "/api/settings/backup/google/files", nil)
	listReq.Header.Set(cliTokenHeader, auth.CLIToken())
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "file-1") {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	restoreBody, _ := json.Marshal(map[string]string{"fileId": "file-1"})
	restoreReq := httptest.NewRequest(http.MethodPost, "/api/settings/backup/google/restore", bytes.NewReader(restoreBody))
	restoreReq.Header.Set(cliTokenHeader, auth.CLIToken())
	restoreReq.Header.Set(backupPassphraseHeader, "restore passphrase 2026")
	restoreReq.Header.Set("Content-Type", "application/json")
	restoreRec := httptest.NewRecorder()
	router.ServeHTTP(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restoreRec.Code, restoreRec.Body.String())
	}

	restored, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if restored["language"] != "es" {
		t.Fatalf("language after restore=%v want es", restored["language"])
	}
	if restored[googleDriveBackupRefreshTokenSetting] != "REFRESH_VALUE" {
		t.Fatalf("restore did not preserve live Drive refresh token")
	}
}
