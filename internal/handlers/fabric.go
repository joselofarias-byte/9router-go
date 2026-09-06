package handlers

import (
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlerutil"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func fabricState(repo *db.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := repo.FabricState(r.Context())
		if err != nil {
			handlerutil.WriteJSONError(w, 500, "Cannot read fabric state")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}
}
func fabricApply(repo *db.Repo, rollback bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Snapshot     *db.FabricEnvelope `json:"snapshot"`
			ExpectedHash *string            `json:"expectedHash"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.ExpectedHash == nil {
			handlerutil.WriteJSONError(w, 400, "Invalid request or missing expectedHash")
			return
		}
		if !rollback {
			if request.Snapshot == nil {
				handlerutil.WriteJSONError(w, 400, "Missing snapshot")
				return
			}
			if _, err := db.ValidateFabric(*request.Snapshot, time.Now(), false); err != nil {
				handlerutil.WriteJSONError(w, 400, err.Error())
				return
			}
		}
		result, err := repo.ApplyFabric(r.Context(), request.Snapshot, *request.ExpectedHash, rollback)
		if err != nil {
			code := http.StatusConflict
			if errors.Is(err, db.ErrFabricConflict) {
				handlerutil.WriteJSONError(w, code, err.Error())
			} else {
				handlerutil.WriteJSONError(w, code, "Snapshot transaction rejected; state preserved")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
func fabricAccounts(repo *db.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accounts, err := repo.GetProviderConnections("", false)
		if err != nil {
			handlerutil.WriteJSONError(w, 500, "Cannot list accounts")
			return
		}
		// Allowlist only. No names, emails, credential data, or tokens leave the gateway.
		result := []map[string]any{}
		for _, a := range accounts {
			locked, err := repo.IsConnectionModelLocked(a.ID, r.URL.Query().Get("model"))
			if err != nil {
				handlerutil.WriteJSONError(w, 500, "Cannot read account status")
				return
			}
			result = append(result, map[string]any{"id": a.ID, "provider": a.Provider, "active": a.IsActive == 1, "priority": a.Priority, "locked": locked})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func fabricRotation(repo *db.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Provider string `json:"provider"`
			Strategy string `json:"strategy"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || request.Provider == "" || strings.ContainsAny(request.Provider, "\"\\\r\n") || (request.Strategy != "none" && request.Strategy != "round-robin" && request.Strategy != "random") {
			handlerutil.WriteJSONError(w, 400, "Invalid rotation policy")
			return
		}
		accounts, err := repo.GetProviderConnections(request.Provider, false)
		if err != nil || len(accounts) == 0 {
			handlerutil.WriteJSONError(w, 400, "Provider must have existing accounts")
			return
		}
		path := `$.providerStrategies."` + request.Provider + `".rotateStrategy`
		_, err = repo.RawDB().ExecContext(r.Context(), `INSERT INTO settings(id,data) VALUES(1,json_set('{}',?,?)) ON CONFLICT(id) DO UPDATE SET data=json_set(settings.data,?,?)`, path, request.Strategy, path, request.Strategy)
		if err != nil {
			handlerutil.WriteJSONError(w, 500, "Cannot set rotation policy")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(request)
	}
}
