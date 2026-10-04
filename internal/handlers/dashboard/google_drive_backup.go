package dashboard

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"9router/proxy/internal/config"
	"9router/proxy/internal/handlerutil"
)

const (
	googleDriveBackupScope      = "https://www.googleapis.com/auth/drive.file"
	googleDriveBackupFolderName = "9router Backups"
	googleDriveBackupKeep       = 5
	googleDriveBackupKind       = "9router-encrypted-backup"
)

var (
	googleDriveBackupAuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	googleDriveBackupTokenURL  = "https://oauth2.googleapis.com/token"
	googleDriveBackupAPIURL    = "https://www.googleapis.com/drive/v3"
	googleDriveBackupUploadURL = "https://www.googleapis.com/upload/drive/v3"
	googleDriveBackupClient    = &http.Client{Timeout: 30 * time.Second}
	googleDriveBackupStates    sync.Map
)

type googleBackupState struct {
	RedirectURI string
	ExpiresAt   time.Time
}

type googleDriveTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type googleDriveFile struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	CreatedTime   string            `json:"createdTime"`
	Size          string            `json:"size"`
	AppProperties map[string]string `json:"appProperties"`
}

func googleDriveBackupCredentials() (clientID, clientSecret string) {
	return strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_BACKUP_CLIENT_ID")),
		strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET"))
}

func googleDriveBackupConfigured() bool {
	id, secret := googleDriveBackupCredentials()
	return id != "" && secret != ""
}

func googleDriveDefaultRedirectURI() string {
	port := config.LoadConfig().Port
	if port <= 0 {
		port = 20130
	}
	return fmt.Sprintf("http://localhost:%d/callback", port)
}

func validateGoogleBackupRedirectURI(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("invalid redirectUri")
	}
	if u.Scheme == "http" {
		host := strings.ToLower(u.Hostname())
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return errors.New("http redirectUri is only allowed for localhost")
		}
	}
	return nil
}

func issueGoogleBackupState(redirectURI string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	googleDriveBackupStates.Store(state, googleBackupState{
		RedirectURI: redirectURI,
		ExpiresAt:   now.Add(10 * time.Minute),
	})
	googleDriveBackupStates.Range(func(key, value any) bool {
		s, ok := value.(googleBackupState)
		if !ok || now.After(s.ExpiresAt) {
			googleDriveBackupStates.Delete(key)
		}
		return true
	})
	return state, nil
}

func consumeGoogleBackupState(state, redirectURI string) bool {
	value, ok := googleDriveBackupStates.LoadAndDelete(strings.TrimSpace(state))
	if !ok {
		return false
	}
	expected, ok := value.(googleBackupState)
	return ok && time.Now().Before(expected.ExpiresAt) && expected.RedirectURI == strings.TrimSpace(redirectURI)
}

// HandleGoogleBackupAuthorize starts a one-shot Google Drive authorization.
// The narrow drive.file scope can only access files the app creates/uses; it
// does not grant blanket read access to the user's Drive.
func (h *DashboardHandler) HandleGoogleBackupAuthorize(w http.ResponseWriter, r *http.Request) {
	if !trustedRequest(r) && !h.verifyDashboardPassword(r.Header.Get(passwordHeader)) {
		writePlainError(w, http.StatusUnauthorized, "Invalid password")
		return
	}
	clientID, clientSecret := googleDriveBackupCredentials()
	if clientID == "" || clientSecret == "" {
		writePlainError(w, http.StatusServiceUnavailable, "Google Drive backup OAuth is not configured")
		return
	}

	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	if redirectURI == "" {
		redirectURI = googleDriveDefaultRedirectURI()
	}
	if err := validateGoogleBackupRedirectURI(redirectURI); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := issueGoogleBackupState(redirectURI)
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "Failed to initialize Google Drive authorization")
		return
	}

	q := url.Values{
		"client_id":              {clientID},
		"redirect_uri":           {redirectURI},
		"response_type":          {"code"},
		"scope":                  {googleDriveBackupScope},
		"access_type":            {"online"},
		"include_granted_scopes": {"true"},
		"prompt":                 {"select_account"},
		"state":                  {state},
	}
	authURL := googleDriveBackupAuthURL + "?" + q.Encode()
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"url":         authURL,
		"authUrl":     authURL,
		"state":       state,
		"redirectUri": redirectURI,
		"scope":       googleDriveBackupScope,
		"persistent":  false,
	})
}

// HandleGoogleBackupUpload exchanges one authorization code, creates an
// encrypted .9rbak entirely in memory, uploads only the ciphertext to a
// user-visible Drive folder, and rotates app-created backups to the newest 5.
// OAuth tokens are intentionally not persisted in v1.
func (h *DashboardHandler) HandleGoogleBackupUpload(w http.ResponseWriter, r *http.Request) {
	if !trustedRequest(r) && !h.verifyDashboardPassword(r.Header.Get(passwordHeader)) {
		writePlainError(w, http.StatusUnauthorized, "Invalid password")
		return
	}
	if !googleDriveBackupConfigured() {
		writePlainError(w, http.StatusServiceUnavailable, "Google Drive backup OAuth is not configured")
		return
	}
	passphrase := backupPassphraseFromRequest(r)
	if err := validateBackupPassphrase(passphrase); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body struct {
		Code        string `json:"code"`
		RedirectURI string `json:"redirectUri"`
		State       string `json:"state"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(raw, &body) != nil {
		writePlainError(w, http.StatusBadRequest, "Invalid Google Drive backup request")
		return
	}
	body.Code = cleanGoogleBackupAuthCode(body.Code)
	body.RedirectURI = strings.TrimSpace(body.RedirectURI)
	if body.Code == "" || validateGoogleBackupRedirectURI(body.RedirectURI) != nil {
		writePlainError(w, http.StatusBadRequest, "Missing authorization code or invalid redirectUri")
		return
	}
	if !consumeGoogleBackupState(body.State, body.RedirectURI) {
		writePlainError(w, http.StatusBadRequest, "Invalid or expired OAuth state")
		return
	}

	accessToken, err := exchangeGoogleBackupCode(r.Context(), body.Code, body.RedirectURI)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, err.Error())
		return
	}

	payload, err := h.exportDatabase()
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "Failed to export database")
		return
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "Failed to encode database backup")
		return
	}
	defer clear(plaintext)
	encrypted, err := encryptBackup(plaintext, passphrase)
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "Failed to encrypt database backup")
		return
	}

	folderID, err := ensureGoogleBackupFolder(r.Context(), accessToken)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to prepare Google Drive backup folder")
		return
	}
	name := "9router-backup-" + time.Now().UTC().Format("20060102T150405Z") + ".9rbak"
	uploaded, err := uploadGoogleBackup(r.Context(), accessToken, folderID, name, encrypted)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to upload encrypted backup to Google Drive")
		return
	}

	deleted, rotateErr := rotateGoogleBackups(r.Context(), accessToken, folderID, googleDriveBackupKeep)
	response := map[string]any{
		"success":        true,
		"fileId":         uploaded.ID,
		"name":           uploaded.Name,
		"folder":         googleDriveBackupFolderName,
		"encrypted":      true,
		"retention":      googleDriveBackupKeep,
		"rotatedDeleted": deleted,
		"oauthPersisted": false,
	}
	if rotateErr != nil {
		response["rotationWarning"] = "Backup uploaded, but old-backup rotation was incomplete"
	}
	handlerutil.WriteJSON(w, http.StatusOK, response)
}

func cleanGoogleBackupAuthCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if code := u.Query().Get("code"); code != "" {
			return code
		}
	}
	if strings.HasPrefix(raw, "code=") {
		if values, err := url.ParseQuery(raw); err == nil && values.Get("code") != "" {
			return values.Get("code")
		}
	}
	return raw
}

func exchangeGoogleBackupCode(ctx context.Context, code, redirectURI string) (string, error) {
	clientID, clientSecret := googleDriveBackupCredentials()
	form := url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleDriveBackupTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("Failed to create Google token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := googleDriveBackupClient.Do(req)
	if err != nil {
		return "", errors.New("Google token exchange failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google token exchange returned status %d", resp.StatusCode)
	}
	var token googleDriveTokenResponse
	if err := json.Unmarshal(data, &token); err != nil || token.AccessToken == "" {
		return "", errors.New("Google token response did not contain an access token")
	}
	return token.AccessToken, nil
}

func ensureGoogleBackupFolder(ctx context.Context, accessToken string) (string, error) {
	q := url.Values{
		"spaces":   {"drive"},
		"q":        {"name = '" + googleDriveBackupFolderName + "' and mimeType = 'application/vnd.google-apps.folder' and trashed = false"},
		"fields":   {"files(id,name)"},
		"pageSize": {"10"},
	}
	req, err := googleDriveRequest(ctx, http.MethodGet, googleDriveBackupAPIURL+"/files?"+q.Encode(), accessToken, nil, "")
	if err != nil {
		return "", err
	}
	resp, err := googleDriveBackupClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list backup folders: status %d", resp.StatusCode)
	}
	var listed struct {
		Files []googleDriveFile `json:"files"`
	}
	if err := decodeGoogleDriveJSON(resp.Body, 1<<20, &listed); err != nil {
		return "", err
	}
	if len(listed.Files) > 0 && listed.Files[0].ID != "" {
		return listed.Files[0].ID, nil
	}

	meta, _ := json.Marshal(map[string]any{
		"name":     googleDriveBackupFolderName,
		"mimeType": "application/vnd.google-apps.folder",
	})
	createReq, err := googleDriveRequest(ctx, http.MethodPost, googleDriveBackupAPIURL+"/files?fields=id,name", accessToken, bytes.NewReader(meta), "application/json")
	if err != nil {
		return "", err
	}
	createResp, err := googleDriveBackupClient.Do(createReq)
	if err != nil {
		return "", err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create backup folder: status %d", createResp.StatusCode)
	}
	var folder googleDriveFile
	if err := decodeGoogleDriveJSON(createResp.Body, 1<<20, &folder); err != nil || folder.ID == "" {
		return "", errors.New("Google Drive did not return backup folder id")
	}
	return folder.ID, nil
}

func uploadGoogleBackup(ctx context.Context, accessToken, folderID, name string, encrypted []byte) (*googleDriveFile, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/json; charset=UTF-8")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return nil, err
	}
	meta, _ := json.Marshal(map[string]any{
		"name":     name,
		"parents":  []string{folderID},
		"mimeType": backupContentType,
		"appProperties": map[string]string{
			"kind":   googleDriveBackupKind,
			"format": backupEncryptedMagic,
		},
	})
	if _, err := metaPart.Write(meta); err != nil {
		return nil, err
	}

	mediaHeader := make(textproto.MIMEHeader)
	mediaHeader.Set("Content-Type", backupContentType)
	mediaPart, err := writer.CreatePart(mediaHeader)
	if err != nil {
		return nil, err
	}
	if _, err := mediaPart.Write(encrypted); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	uploadURL := googleDriveBackupUploadURL + "/files?uploadType=multipart&fields=id,name,createdTime,size,appProperties"
	req, err := googleDriveRequest(ctx, http.MethodPost, uploadURL, accessToken, &body, "multipart/related; boundary="+writer.Boundary())
	if err != nil {
		return nil, err
	}
	resp, err := googleDriveBackupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload backup: status %d", resp.StatusCode)
	}
	var file googleDriveFile
	if err := decodeGoogleDriveJSON(resp.Body, 1<<20, &file); err != nil || file.ID == "" {
		return nil, errors.New("Google Drive did not return uploaded file id")
	}
	return &file, nil
}

func rotateGoogleBackups(ctx context.Context, accessToken, folderID string, keep int) (int, error) {
	if keep < 1 {
		return 0, nil
	}
	q := url.Values{
		"spaces":  {"drive"},
		"q":       {"'" + folderID + "' in parents and trashed = false"},
		"orderBy": {"createdTime desc"},
		"fields":  {"files(id,name,createdTime,size,appProperties)"},
		"pageSize": {"100"},
	}
	req, err := googleDriveRequest(ctx, http.MethodGet, googleDriveBackupAPIURL+"/files?"+q.Encode(), accessToken, nil, "")
	if err != nil {
		return 0, err
	}
	resp, err := googleDriveBackupClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("list backups: status %d", resp.StatusCode)
	}
	var listed struct {
		Files []googleDriveFile `json:"files"`
	}
	if err := decodeGoogleDriveJSON(resp.Body, 2<<20, &listed); err != nil {
		return 0, err
	}

	ours := make([]googleDriveFile, 0, len(listed.Files))
	for _, file := range listed.Files {
		if file.AppProperties["kind"] == googleDriveBackupKind && strings.HasSuffix(file.Name, ".9rbak") {
			ours = append(ours, file)
		}
	}
	deleted := 0
	var firstErr error
	for _, file := range ours[keep:] {
		delReq, err := googleDriveRequest(ctx, http.MethodDelete, googleDriveBackupAPIURL+"/files/"+url.PathEscape(file.ID), accessToken, nil, "")
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delResp, err := googleDriveBackupClient.Do(delReq)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		io.Copy(io.Discard, io.LimitReader(delResp.Body, 1<<20))
		delResp.Body.Close()
		if delResp.StatusCode != http.StatusNoContent {
			if firstErr == nil {
				firstErr = fmt.Errorf("delete old backup %s: status %d", file.ID, delResp.StatusCode)
			}
			continue
		}
		deleted++
	}
	return deleted, firstErr
}

func decodeGoogleDriveJSON(r io.Reader, limit int64, dst any) error {
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func googleDriveRequest(ctx context.Context, method, target, accessToken string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}
