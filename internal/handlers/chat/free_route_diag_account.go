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

	availabilityUnknown       = "unknown"
	availabilityRecentSuccess = "recent_success"
)

type accountIndex struct {
	byProvider map[string][]FreeRouteAccount
}

func (s accountIndex) forProvider(provider string) []FreeRouteAccount {
	src := s.byProvider[provider]
	if len(src) == 0 {
		return []FreeRouteAccount{}
	}
	return append([]FreeRouteAccount(nil), src...)
}

func (h *ChatHandler) accountsByProvider() accountIndex {
	idx := accountIndex{byProvider: map[string][]FreeRouteAccount{}}
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
		idx.byProvider[conn.Provider] = append(idx.byProvider[conn.Provider], classifyAccount(conn.ID, conn.IsActive == 1, conn.Data))
	}
	for provider, accs := range idx.byProvider {
		slices.SortFunc(accs, func(a, b FreeRouteAccount) int {
			if a.AccountID < b.AccountID {
				return -1
			}
			if a.AccountID > b.AccountID {
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
