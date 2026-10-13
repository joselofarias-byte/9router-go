package chat

import (
	"net/http"

	"9router/proxy/internal/handlerutil"
)

// HandleConnectedModels supplies the comparator picker from the gateway's
// existing connected catalog, including noAuth providers. It never falls back
// to the full catalog on an empty installation or adds synthetic fabric IDs.
func (h *ChatHandler) HandleConnectedModels(w http.ResponseWriter, r *http.Request) {
	result := h.buildModelsListResult(r.Context(), modeListConnected)
	w.Header().Set("Cache-Control", "no-store")
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"data": result.Models, "mode": result.Mode, "connections": result.Connections,
	})
}
