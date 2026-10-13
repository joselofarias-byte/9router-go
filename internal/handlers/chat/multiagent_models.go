package chat

import (
	"net/http"
	"sort"
	"strings"

	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/providers"
)

type modelCandidate struct {
	ID       string `json:"id"`
	OwnedBy  string `json:"owned_by"`
	Provider string `json:"provider"`
}

// HandleConnectedModels supplies the comparator picker from the gateway's
// existing connected catalog, including noAuth providers. It never falls back
// to the full catalog on an empty installation or adds synthetic fabric IDs.
func (h *ChatHandler) HandleConnectedModels(w http.ResponseWriter, r *http.Request) {
	result := h.buildModelsListResult(r.Context(), modeListConnected)
	w.Header().Set("Cache-Control", "no-store")
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"data": normalizeModelCandidates(result.Models), "mode": result.Mode, "connections": result.Connections,
	})
}

// Genuine aliases can publish the same model twice (e.g. mmf/ and mimo-free/).
// Keep one existing wire ID per actual provider/model, preferring its full
// provider prefix. Never collapse models from different providers or rewrite
// an ID into a target that the connected catalog did not publish.
func normalizeModelCandidates(models []ModelInfoObject) []modelCandidate {
	unique := make(map[string]modelCandidate, len(models))
	for _, model := range models {
		provider := providers.ResolveAlias(model.OwnedBy)
		name := strings.TrimPrefix(model.ID, model.OwnedBy+"/")
		key := provider + "\x00" + name
		candidate := modelCandidate{ID: model.ID, OwnedBy: model.OwnedBy, Provider: provider}
		current, exists := unique[key]
		canonicalID := provider + "/" + name
		if !exists || candidate.ID == canonicalID || (current.ID != canonicalID && candidate.ID < current.ID) {
			unique[key] = candidate
		}
	}
	data := make([]modelCandidate, 0, len(unique))
	for _, candidate := range unique {
		data = append(data, candidate)
	}
	sort.Slice(data, func(i, j int) bool {
		if data[i].Provider != data[j].Provider {
			return data[i].Provider < data[j].Provider
		}
		return data[i].ID < data[j].ID
	})
	return data
}
