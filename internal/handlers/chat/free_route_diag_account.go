package chat

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"9router/proxy/internal/db"
)

const (
	quotaUnknown     = "unknown"
	quotaExhausted   = "quota_exhausted"
	quotaRateLimited = "rate_limited"

	accountStateEligible       = "eligible"
	accountStateInactive       = "inactive"
	accountStateAuthFailure    = "auth_failure"
	accountStateQuotaExhausted = "quota_exhausted"
	accountStateRateLimited    = "rate_limited"
	accountStateGateway        = "gateway_temporary"
	accountStateCooldown       = "cooldown_unclassified"
	accountStateModelLocked    = "model_locked"

	availabilityUnknown       = "unknown"
	availabilityRecentSuccess = "recent_success"
)

type storedAccount struct {
	id     string
	active bool
	raw    string
}

type accountIndex struct {
	byProvider map[string][]storedAccount
}

func (s accountIndex) forModel(provider, model string) []FreeRouteAccount {
	src := s.byProvider[provider]
	if len(src) == 0 {
		return []FreeRouteAccount{}
	}
	out := make([]FreeRouteAccount, 0, len(src))
	for _, row := range src {
		out = append(out, classifyAccountForModel(row.id, row.active, row.raw, provider, model))
	}
	return out
}

func (h *ChatHandler) accountsByProvider() accountIndex {
	idx := accountIndex{byProvider: map[string][]storedAccount{}}
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
		idx.byProvider[conn.Provider] = append(idx.byProvider[conn.Provider], storedAccount{
			id: conn.ID, active: conn.IsActive == 1, raw: conn.Data,
		})
	}
	for provider, accs := range idx.byProvider {
		slices.SortFunc(accs, func(a, b storedAccount) int {
			if a.id < b.id {
				return -1
			}
			if a.id > b.id {
				return 1
			}
			return 0
		})
		idx.byProvider[provider] = accs
	}
	return idx
}

// classifyAccount reads one connection blob. A missing cooldown leaves quota
// unknown. A 401/403 or a gateway status records that condition and still
// leaves quota unknown, because those responses do not say how much balance
// remains.
func classifyAccount(id string, active bool, raw string) FreeRouteAccount {
	acct := FreeRouteAccount{AccountID: id, State: accountStateEligible, Quota: quotaUnknown}
	if !active {
		acct.State = accountStateInactive
		return acct
	}
	until, cooling := db.ConnectionCooldownUntil(raw)
	if !cooling || !until.After(time.Now()) {
		return acct
	}
	switch lastErrorStatus(raw) {
	case http.StatusUnauthorized, http.StatusForbidden:
		acct.State = accountStateAuthFailure
	case http.StatusPaymentRequired:
		acct.State = accountStateQuotaExhausted
		acct.Quota = quotaExhausted
		acct.QuotaKnown = true
	case http.StatusTooManyRequests:
		acct.State = accountStateRateLimited
		acct.Quota = quotaRateLimited
		acct.QuotaKnown = true
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		acct.State = accountStateGateway
	default:
		acct.State = accountStateCooldown
	}
	return acct
}

// classifyAccountForModel applies a per-model lock on top of the account-wide
// cooldown. The lock is not a quota reading: quota stays unknown.
func classifyAccountForModel(id string, active bool, raw, provider, model string) FreeRouteAccount {
	acct := classifyAccount(id, active, raw)
	if acct.State != accountStateEligible || !modelLocked(raw, provider, model) {
		return acct
	}
	acct.State = accountStateModelLocked
	acct.Quota = quotaUnknown
	acct.QuotaKnown = false
	return acct
}

func modelLocked(raw, provider, model string) bool {
	if db.ConnectionModelLocked(raw, model) {
		return true
	}
	canon := canonicalLockModel(provider, model)
	return canon != model && db.ConnectionModelLocked(raw, canon)
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
