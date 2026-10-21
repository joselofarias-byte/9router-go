package chat

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/providers"
)

// FreeRouteHop is one eligible free route, without credentials.
type FreeRouteHop struct {
	Entry        string `json:"entry"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	AccountID    string `json:"accountId,omitempty"`
	Pricing      string `json:"pricing"`
	Availability string `json:"availability"`
	Quota        string `json:"quota"`
}

// FreeRouteDiscard explains why a catalog row is outside the free pool.
type FreeRouteDiscard struct {
	Provider  string `json:"provider"`
	Model     string `json:"model,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Pricing   string `json:"pricing,omitempty"`
	Reason    string `json:"reason"`
}

// FreeRouteDiagnosis is the admin view of a virtual free route.
// It must stay free of API keys, tokens, cookies, and upstream error text.
type FreeRouteDiagnosis struct {
	Model     string             `json:"model"`
	Policy    string             `json:"policy"`
	Routable  bool               `json:"routable"`
	Selected  []FreeRouteHop     `json:"selected"`
	Discarded []FreeRouteDiscard `json:"discarded"`
	CheckedAt string             `json:"checkedAt"`
}

// HandleFreeBestDiagnosis serves GET /api/admin/free-best?model=free-best.
func (h *ChatHandler) HandleFreeBestDiagnosis(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		model = "free-best"
	}
	if !isVirtualFreeRoute(model) {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "model must be a virtual free route")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, h.ExplainFreeRoute(model))
}

// ExplainFreeRoute reports the ordered free pool and the rows left out of it.
func (h *ChatHandler) ExplainFreeRoute(modelName string) FreeRouteDiagnosis {
	diag := FreeRouteDiagnosis{
		Model:     canonicalVirtualName(modelName),
		Policy:    string(routing.PolicyFreeOnly),
		Selected:  []FreeRouteHop{},
		Discarded: []FreeRouteDiscard{},
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if h == nil || h.Repo == nil || !isVirtualFreeRoute(modelName) {
		return diag
	}
	info, err := h.resolveModel(modelName)
	selected := map[string]bool{}
	if err == nil && info != nil && info.VirtualFree {
		diag.Routable = len(info.ComboModels) > 0
		diag.Selected = h.describeSelectedHops(info.ComboModels)
		for _, hop := range diag.Selected {
			selected[hop.Entry] = true
		}
	}
	diag.Discarded = discardUnselected(registry.GetActiveState(), selected, h.connectionSignals())
	return diag
}

func (h *ChatHandler) describeSelectedHops(entries []string) []FreeRouteHop {
	health := h.collectFreeRouteRecentHealth(entries)
	signals := h.connectionSignals()
	hops := make([]FreeRouteHop, 0, len(entries))
	for _, entry := range entries {
		provider, model := splitRouteEntry(entry)
		signal := signals.forProvider(provider)
		availability := "unknown"
		if stat := health[entry]; stat != nil && stat.successes > 0 {
			availability = "verified"
		}
		hops = append(hops, FreeRouteHop{
			Entry:        entry,
			Provider:     provider,
			Model:        model,
			AccountID:    signal.accountID,
			Pricing:      "free",
			Availability: availability,
			Quota:        signal.quota,
		})
	}
	return hops
}

type accountSignal struct {
	accountID string
	quota     string
	auth      bool
	cooling   bool
	active    bool
}

type signalIndex struct {
	byProvider map[string]accountSignal
}

func (s signalIndex) forProvider(provider string) accountSignal {
	if s.byProvider == nil {
		return accountSignal{quota: "unknown"}
	}
	sig, ok := s.byProvider[provider]
	if !ok {
		return accountSignal{quota: "unknown"}
	}
	return sig
}

func (h *ChatHandler) connectionSignals() signalIndex {
	idx := signalIndex{byProvider: map[string]accountSignal{}}
	if h == nil || h.Repo == nil {
		return idx
	}
	conns, err := h.Repo.GetProviderConnections("", false)
	if err != nil {
		return idx
	}
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		sig := readAccountSignal(conn.ID, conn.IsActive == 1, conn.Data)
		prev, seen := idx.byProvider[conn.Provider]
		if !seen || betterSignal(sig, prev) {
			idx.byProvider[conn.Provider] = sig
		}
	}
	return idx
}

func betterSignal(next, prev accountSignal) bool {
	if next.active != prev.active {
		return next.active
	}
	return !next.cooling && prev.cooling
}

func readAccountSignal(id string, active bool, raw string) accountSignal {
	sig := accountSignal{accountID: id, quota: "unknown", active: active}
	if !active {
		sig.quota = "unknown"
		return sig
	}
	until, cooling := db.ConnectionCooldownUntil(raw)
	if !cooling || !until.After(time.Now()) {
		return sig
	}
	sig.cooling = true
	status := lastErrorStatus(raw)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		sig.auth = true
		sig.quota = "auth_failure"
	case http.StatusPaymentRequired, http.StatusTooManyRequests:
		sig.quota = "known_exhausted"
	default:
		sig.quota = "known_exhausted"
	}
	return sig
}

func lastErrorStatus(raw string) int {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return 0
	}
	last, _ := data["lastError"].(map[string]any)
	if last == nil {
		return 0
	}
	switch status := last["status"].(type) {
	case float64:
		return int(status)
	case int:
		return status
	default:
		return 0
	}
}

func discardUnselected(state *registry.RegistryState, selected map[string]bool, signals signalIndex) []FreeRouteDiscard {
	if state == nil {
		return []FreeRouteDiscard{}
	}
	out := []FreeRouteDiscard{}
	for providerID, models := range state.ProviderModels {
		provider := state.Providers[providerID]
		for _, pm := range models {
			if pm == nil {
				continue
			}
			entry := freeEntryOf(pm)
			if selected[entry] {
				continue
			}
			reason, pricing := discardReason(provider, pm, signals.forProvider(providerID))
			out = append(out, FreeRouteDiscard{
				Provider: pm.ProviderID,
				Model:    pm.ModelID,
				Pricing:  pricing,
				Reason:   reason,
			})
		}
	}
	return out
}

func discardReason(provider *registry.Provider, pm *registry.ProviderModel, signal accountSignal) (string, string) {
	if provider == nil || !provider.IsActive {
		return "inactive_provider", pm.PricingMode
	}
	if !pm.IsActive {
		return "inactive_model", pm.PricingMode
	}
	switch pm.PricingMode {
	case "paid":
		return "paid", pm.PricingMode
	case "unknown", "":
		return "unknown_cost", pm.PricingMode
	case "free", "free_tier":
	default:
		return "not_free", pm.PricingMode
	}
	cfgOK := false
	if cfg, known := providers.KnownProviders[pm.ProviderID]; known && cfg.NoAuth {
		cfgOK = true
	}
	if !signal.active && !cfgOK {
		return "no_account", pm.PricingMode
	}
	if signal.auth {
		return "auth_failure", pm.PricingMode
	}
	if signal.cooling {
		return "cooldown", pm.PricingMode
	}
	return "not_selected", pm.PricingMode
}

func freeEntryOf(pm *registry.ProviderModel) string {
	model := pm.UpstreamModel
	if model == "" {
		model = pm.ModelID
	}
	return pm.ProviderID + "/" + model
}

func splitRouteEntry(entry string) (string, string) {
	parts := strings.SplitN(entry, "/", 2)
	if len(parts) != 2 {
		return entry, ""
	}
	return parts[0], parts[1]
}
