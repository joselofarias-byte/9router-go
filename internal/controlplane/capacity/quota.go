package capacity

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// AccountQuota is a credential-free routing hint for one provider/account/model.
// It deliberately carries no token, email, or other account identity data.
type AccountQuota struct {
	RemainingPercentage float64
	ResetAt              time.Time
	UpdatedAt            time.Time
}

type key struct {
	provider string
	account  string
	model    string
}

var (
	mu     sync.RWMutex
	quotas = map[key]AccountQuota{}

	// MaxAge bounds how long a quota observation influences routing when the
	// provider has not refreshed it. Stale data must never pin routing forever.
	MaxAge = 10 * time.Minute
)

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Update records the latest quota observation for an account/model pair.
func Update(provider, account, model string, remainingPercentage float64, resetAt time.Time) {
	provider = normalize(provider)
	account = strings.TrimSpace(account)
	model = strings.TrimSpace(model)
	if provider == "" || account == "" || model == "" {
		return
	}
	if remainingPercentage < 0 {
		remainingPercentage = 0
	}
	if remainingPercentage > 100 {
		remainingPercentage = 100
	}

	mu.Lock()
	quotas[key{provider: provider, account: account, model: model}] = AccountQuota{
		RemainingPercentage: remainingPercentage,
		ResetAt:              resetAt.UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	mu.Unlock()
}

// Get returns a fresh quota observation. Expired observations are ignored.
func Get(provider, account, model string) (AccountQuota, bool) {
	k := key{provider: normalize(provider), account: strings.TrimSpace(account), model: strings.TrimSpace(model)}
	mu.RLock()
	q, ok := quotas[k]
	mu.RUnlock()
	if !ok {
		return AccountQuota{}, false
	}
	if MaxAge > 0 && time.Since(q.UpdatedAt) > MaxAge {
		return AccountQuota{}, false
	}
	return q, true
}

// IsExhausted reports a known zero-quota account whose reset is still in the future.
func IsExhausted(provider, account, model string) bool {
	q, ok := Get(provider, account, model)
	if !ok || q.RemainingPercentage > 0 {
		return false
	}
	return !q.ResetAt.IsZero() && q.ResetAt.After(time.Now().UTC())
}

// OrderAccounts returns a copy ranked by known remaining quota. Fresh positive
// observations come first, unknown accounts remain available as fallbacks, and
// known exhausted accounts are last. Ties preserve caller order.
func OrderAccounts(provider, model string, accountIDs []string) []string {
	out := append([]string(nil), accountIDs...)
	sort.SliceStable(out, func(i, j int) bool {
		qi, iok := Get(provider, out[i], model)
		qj, jok := Get(provider, out[j], model)

		ri := rank(qi, iok)
		rj := rank(qj, jok)
		if ri.class != rj.class {
			return ri.class > rj.class
		}
		if ri.class == 2 && ri.remaining != rj.remaining {
			return ri.remaining > rj.remaining
		}
		return false
	})
	return out
}

type accountRank struct {
	class     int
	remaining float64
}

func rank(q AccountQuota, ok bool) accountRank {
	if !ok {
		return accountRank{class: 1}
	}
	if q.RemainingPercentage <= 0 && !q.ResetAt.IsZero() && q.ResetAt.After(time.Now().UTC()) {
		return accountRank{class: 0}
	}
	return accountRank{class: 2, remaining: q.RemainingPercentage}
}

// ClearProvider drops routing hints for one provider, without touching credentials.
func ClearProvider(provider string) {
	provider = normalize(provider)
	mu.Lock()
	for k := range quotas {
		if k.provider == provider {
			delete(quotas, k)
		}
	}
	mu.Unlock()
}

// ClearAll is primarily useful in tests.
func ClearAll() {
	mu.Lock()
	quotas = map[key]AccountQuota{}
	mu.Unlock()
}
