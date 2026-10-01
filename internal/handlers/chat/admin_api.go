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
	switch policy {
	case routing.PolicyBalanced, routing.PolicyFreeOnly, routing.PolicyFreeFirst, routing.PolicyTrusted:
	default:
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Unknown routing policy")
		return
	}

	requested := model
	if routing.IsFreeProfile(model) {
		requested, policy = "", routing.PolicyFreeOnly
	}
	var candidates []routing.RouteNode
	if h.Repo != nil {
		candidates = getPolicyCandidates(r.Context(), h.Repo.RawDB(), requested, policy)
	} else {
		candidates = globalRoutingEngine.SelectCandidates(requested, policy)
	}
	if routing.IsFreeProfile(model) {
		candidates = routing.FilterProfile(registry.GetActiveState(), candidates, model)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"requestedModel": model,
		"policy":         policy,
		"candidates":     candidates,
	})
}
