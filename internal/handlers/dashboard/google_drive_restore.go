package dashboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	json "encoding/json/v2"

	"9router/proxy/internal/handlerutil"
)

func listGoogleBackups(ctx context.Context, accessToken, folderID string) ([]googleDriveFile, error) {
	q := url.Values{
		"spaces":   {"drive"},
		"q":        {"'" + folderID + "' in parents and trashed = false"},
		"orderBy":  {"createdTime desc"},
		"fields":   {"files(id,name,createdTime,size,appProperties)"},
		"pageSize": {"100"},
	}
	req, err := googleDriveRequest(ctx, http.MethodGet, googleDriveBackupAPIURL+"/files?"+q.Encode(), accessToken, nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := googleDriveBackupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list backups: status %d", resp.StatusCode)
	}

	var listed struct {
		Files []googleDriveFile `json:"files"`
	}
	if err := decodeGoogleDriveJSON(resp.Body, 2<<20, &listed); err != nil {
		return nil, err
	}

	out := make([]googleDriveFile, 0, len(listed.Files))
	for _, file := range listed.Files {
		if file.ID == "" || !strings.HasSuffix(strings.ToLower(file.Name), ".9rbak") {
			continue
		}
		if file.AppProperties["kind"] != googleDriveBackupKind {
			continue
		}
		out = append(out, file)
	}
	return out, nil
}

// HandleGoogleBackupList returns only metadata for backups created by 9router-go.
func (h *DashboardHandler) HandleGoogleBackupList(w http.ResponseWriter, r *http.Request) {
	if !trustedRequest(r) && !h.verifyDashboardPassword(r.Header.Get(passwordHeader)) {
		writePlainError(w, http.StatusUnauthorized, "Invalid password")
		return
	}

	accessToken, err := h.googleDriveBackupAccessToken(r.Context())
	if err != nil {
		writePlainError(w, http.StatusUnauthorized, err.Error())
		return
	}
	folderID, err := ensureGoogleBackupFolder(r.Context(), accessToken)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to open Google Drive backup folder")
		return
	}
	files, err := listGoogleBackups(r.Context(), accessToken, folderID)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to list Google Drive backups")
		return
	}

	type publicFile struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		CreatedTime string `json:"createdTime"`
		Size        string `json:"size"`
	}
	public := make([]publicFile, 0, len(files))
	for _, file := range files {
		public = append(public, publicFile{
			ID: file.ID, Name: file.Name, CreatedTime: file.CreatedTime, Size: file.Size,
		})
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"folder": googleDriveBackupFolderName,
		"files":  public,
	})
}

func downloadGoogleBackup(ctx context.Context, accessToken, fileID string) ([]byte, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, errors.New("missing Google Drive file id")
	}
	target := googleDriveBackupAPIURL + "/files/" + url.PathEscape(fileID) + "?alt=media"
	req, err := googleDriveRequest(ctx, http.MethodGet, target, accessToken, nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := googleDriveBackupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download backup: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("Google Drive backup is empty")
	}
	return data, nil
}

// HandleGoogleBackupRestore downloads one app-created encrypted backup from
// Drive, decrypts it in memory, and restores it without writing plaintext to disk.
func (h *DashboardHandler) HandleGoogleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if !trustedRequest(r) && !h.verifyDashboardPassword(r.Header.Get(passwordHeader)) {
		writePlainError(w, http.StatusUnauthorized, "Invalid password")
		return
	}
	passphrase := backupPassphraseFromRequest(r)
	if err := validateBackupPassphrase(passphrase); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body struct {
		FileID string `json:"fileId"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(raw, &body) != nil || strings.TrimSpace(body.FileID) == "" {
		writePlainError(w, http.StatusBadRequest, "Missing Google Drive backup file")
		return
	}

	accessToken, err := h.googleDriveBackupAccessToken(r.Context())
	if err != nil {
		writePlainError(w, http.StatusUnauthorized, err.Error())
		return
	}
	folderID, err := ensureGoogleBackupFolder(r.Context(), accessToken)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to open Google Drive backup folder")
		return
	}
	files, err := listGoogleBackups(r.Context(), accessToken, folderID)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to list Google Drive backups")
		return
	}

	var selected *googleDriveFile
	for i := range files {
		if files[i].ID == body.FileID {
			selected = &files[i]
			break
		}
	}
	if selected == nil {
		writePlainError(w, http.StatusNotFound, "Google Drive backup was not found")
		return
	}

	encrypted, err := downloadGoogleBackup(r.Context(), accessToken, selected.ID)
	if err != nil {
		writePlainError(w, http.StatusBadGateway, "Failed to download Google Drive backup")
		return
	}
	if !isEncryptedBackup(encrypted) {
		writePlainError(w, http.StatusBadRequest, "Selected Google Drive file is not an encrypted 9router backup")
		return
	}

	plaintext, err := decryptBackup(encrypted, passphrase)
	if err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer clear(plaintext)

	var payload map[string]any
	if err := json.Unmarshal(plaintext, &payload); err != nil || payload == nil {
		writePlainError(w, http.StatusBadRequest, "Invalid encrypted database payload")
		return
	}
	if err := h.importDatabase(payload); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"name":    selected.Name,
	})
}
