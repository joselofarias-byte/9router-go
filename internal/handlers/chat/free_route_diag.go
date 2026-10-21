package chat

import (
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/routing"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/providers"
)

// FreeRouteAccount is one provider connection as the free-best selector sees it.
// Quota is never a number: an unknown remaining balance stays "unknown" and
// quotaKnown is false. It is not reported as zero and not reported as verified
// capacity. Auth and gateway cooldowns do not change that.
type FreeRouteAccount struct {
	AccountID  string `json:"accountId"`
	State      string `json:"state"`
	Quota      string `json:"quota"`
	QuotaKnown bool   `json:"quotaKnown"`
}

// FreeRouteHop is one eligible free route, without credentials.
// Availability is recent delivery evidence only. It is not a quota reading.
type FreeRouteHop struct {
	Entry        string             `json:"entry"`
	Provider     string             `json:"provider"`
	Model        string             `json:"model"`
	Pricing      string             `json:"pricing"`
	Availability string             `json:"availability"`
	Accounts     []FreeRouteAccount `json:"accounts"`
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
// It must stay free of API keys, tokens, cookies, emails, and upstream error text.
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
	diag.Discarded = discardUnselected(registry.GetActiveState(), selected, h.accountsByProvider())
	return diag
}

func (h *ChatHandler) describeSelectedHops(entries []string) []FreeRouteHop {
	health := h.collectFreeRouteRecentHealth(entries)
	accounts := h.accountsByProvider()
	state := registry.GetActiveState()
	hops := make([]FreeRouteHop, 0, len(entries))
	for _, entry := range entries {
		provider, model := splitRouteEntry(entry)
		hops = append(hops, FreeRouteHop{
			Entry:        entry,
			Provider:     provider,
			Model:        model,
			Pricing:      pricingModeForEntry(state, provider, model),
			Availability: hopAvailability(health[entry]),
			Accounts:     accountsForHop(provider, accounts),
		})
	}
	return hops
}

func hopAvailability(stat *freeRouteRecentHealth) string {
	if stat != nil && stat.successes > 0 {
		return availabilityRecentSuccess
	}
	return availabilityUnknown
}

func accountsForHop(provider string, accounts accountIndex) []FreeRouteAccount {
	listed := accounts.forProvider(provider)
	if len(listed) > 0 {
		return listed
	}
	cfg, known := providers.KnownProviders[provider]
	if !known || !cfg.NoAuth {
		return []FreeRouteAccount{}
	}
	return []FreeRouteAccount{{
		AccountID:  routing.VirtualNoAuthAccountID(provider),
		State:      accountStateEligible,
		Quota:      quotaUnknown,
		QuotaKnown: false,
	}}
}

func pricingModeForEntry(state *registry.RegistryState, provider, model string) string {
	if state == nil || provider == "" || model == "" {
		return ""
	}
	fallback := ""
	foundFallback := false
	for _, pm := range state.ProviderModels[provider] {
		if pm == nil {
			continue
		}
		upstream := pm.UpstreamModel
		if upstream == "" {
			upstream = pm.ModelID
		}
		if upstream == model {
			return pm.PricingMode
		}
		if pm.ModelID == model && !foundFallback {
			fallback = pm.PricingMode
			foundFallback = true
		}
	}
	if foundFallback {
		return fallback
	}
	return ""
}

func discardUnselected(state *registry.RegistryState, selected map[string]bool, accounts accountIndex) []FreeRouteDiscard {
	if state == nil {
		return []FreeRouteDiscard{}
	}
	out := []FreeRouteDiscard{}
	for providerID, models := range state.ProviderModels {
		provider := state.Providers[providerID]
		for _, pm := range models {
			if pm == nil || selected[freeEntryOf(pm)] {
				continue
			}
			out = append(out, discardsForModel(provider, pm, accounts)...)
		}
	}
	return out
}

func discardsForModel(provider *registry.Provider, pm *registry.ProviderModel, accounts accountIndex) []FreeRouteDiscard {
	if reason, modelLevel := modelDiscardReason(provider, pm); modelLevel {
		return []FreeRouteDiscard{{
			Provider: pm.ProviderID,
			Model:    pm.ModelID,
			Pricing:  pm.PricingMode,
			Reason:   reason,
		}}
	}
	return accountDiscards(pm, accounts)
}

func modelDiscardReason(provider *registry.Provider, pm *registry.ProviderModel) (string, bool) {
	if provider == nil || !provider.IsActive {
		return "inactive_provider", true
	}
	if !pm.IsActive {
		return "inactive_model", true
	}
	switch pm.PricingMode {
	case "paid":
		return "paid", true
	case "unknown", "":
		return "unknown_cost", true
	case "free", "free_tier":
		return "", false
	default:
		return "not_free", true
	}
}

func accountDiscards(pm *registry.ProviderModel, accounts accountIndex) []FreeRouteDiscard {
	cfg, known := providers.KnownProviders[pm.ProviderID]
	noAuth := known && cfg.NoAuth
	out := []FreeRouteDiscard{}
	anyEligible := false
	for _, acct := range accounts.forProvider(pm.ProviderID) {
		switch acct.State {
		case accountStateEligible:
			anyEligible = true
		case accountStateInactive:
			continue
		default:
			out = append(out, FreeRouteDiscard{
				Provider:  pm.ProviderID,
				Model:     pm.ModelID,
				AccountID: acct.AccountID,
				Pricing:   pm.PricingMode,
				Reason:    acct.State,
			})
		}
	}
	if anyEligible || noAuth {
		return append(out, FreeRouteDiscard{
			Provider: pm.ProviderID,
			Model:    pm.ModelID,
			Pricing:  pm.PricingMode,
			Reason:   "not_selected",
		})
	}
	if len(out) == 0 {
		out = append(out, FreeRouteDiscard{
			Provider: pm.ProviderID,
			Model:    pm.ModelID,
			Pricing:  pm.PricingMode,
			Reason:   "no_account",
		})
	}
	return out
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
