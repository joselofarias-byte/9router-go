package chat

import (
	"net/http"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/handlerutil"
)

// HandleAdminRegistry returns the active snapshot payload in memory.
func (h *ChatHandler) HandleAdminRegistry(w http.ResponseWriter, r *http.Request) {
	state := registry.GetActiveState()
	if state == nil {
		handlerutil.WriteJSONError(w, http.StatusServiceUnavailable, "Registry not initialized")
		return
	}

	// AuthData has been safely removed from the Registry schema,
	// so the snapshot payload is safe to return directly.
	handlerutil.WriteJSON(w, http.StatusOK, state)
}

// HandleAdminExplainRoute explains routing logic for a model and policy.
func (h *ChatHandler) HandleAdminExplainRoute(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	policyParam := r.URL.Query().Get("policy")
	if model == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Missing 'model' parameter")
		return
	}

	policy := routing.PolicyBalanced
	if policyParam != "" {
		policy = routing.Policy(policyParam)
	}

	// Reuse the shared routing engine/trust manager so this explains the
	// exact same trust levels and scores a real request would see, instead
	// of a fresh manager that always reports every node as unknown/untested.
	candidates := globalRoutingEngine.SelectCandidates(model, policy)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"requestedModel": model,
		"policy":         policy,
		"candidates":     candidates,
	})
}

// HandleAdminSnapshots lists registry snapshot history (version, reason,
// status, timestamp — no payload) so an operator can see what's available to
// roll back to before choosing a version.
// GET /admin/registry/snapshots
func (h *ChatHandler) HandleAdminSnapshots(w http.ResponseWriter, r *http.Request) {
	if h.Repo == nil || h.Repo.RawDB() == nil {
		handlerutil.WriteJSONError(w, http.StatusServiceUnavailable, "database not available")
		return
	}
	snaps, err := registry.ListSnapshots(h.Repo.RawDB(), 50)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"snapshots": snaps})
}

// HandleAdminRollback activates an earlier registry snapshot on demand — the
// manual counterpart to the automatic last-known-good recovery InitRegistry
// performs on a corrupt snapshot at startup. Pass ?version=<id> to roll back
// to a specific snapshot (from HandleAdminSnapshots), or omit it to roll back
// to the most recent last_known_good snapshot.
// POST /admin/registry/rollback?version=
func (h *ChatHandler) HandleAdminRollback(w http.ResponseWriter, r *http.Request) {
	if h.Repo == nil || h.Repo.RawDB() == nil {
		handlerutil.WriteJSONError(w, http.StatusServiceUnavailable, "database not available")
		return
	}
	db := h.Repo.RawDB()

	version := r.URL.Query().Get("version")
	if version == "" {
		lkg, err := registry.GetLastKnownGoodSnapshot(db)
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusNotFound, "no last_known_good snapshot available: "+err.Error())
			return
		}
		version = lkg.Version
	}

	if err := registry.ActivateSnapshot(db, version); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "activatedVersion": version})
}
