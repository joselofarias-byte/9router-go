// Package capacity holds reported quota separately from observed retry delays.
// Adapted from PR #28; no credentials, inferred balances or aggregated quotas.
package capacity

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxAge = 10 * time.Minute

type Status string

const (
	Unknown   Status = "unknown"
	Stale     Status = "stale"
	Available Status = "available"
	Exhausted Status = "exhausted"
)

type AccountQuota struct {
	RemainingPercentage float64
	ResetAt             time.Time
	UpdatedAt           time.Time
}

// Scope is explicit provider metadata, never guessed from an account name.
// Identifiers stay in memory and are not exposed in routing diagnostics.
type Scope struct{ Project, Organization string }
type accountKey struct{ provider, account string }
type key struct {
	provider, account, model string
	scope                    Scope
}
type Snapshot struct {
	Status              Status    `json:"status"`
	RemainingPercentage float64   `json:"remainingPercentage"`
	UpdatedAt           time.Time `json:"updatedAt"`
	ResetAt             time.Time `json:"resetAt"`
	BlockedUntil        time.Time `json:"blockedUntil"`
}

var (
	mu     sync.RWMutex
	quotas = map[key]AccountQuota{}
	scopes = map[accountKey]Scope{}
	blocks = map[key]time.Time{}
)

func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func quotaKey(provider, account, model string) key {
	p, a := normalize(provider), strings.TrimSpace(account)
	s := scopes[accountKey{p, a}]
	if s != (Scope{}) {
		a = ""
	}
	return key{p, a, strings.TrimSpace(model), s}
}

// SetScope associates accounts with a shared limit only when explicitly known.
// It does not copy or sum account-local observations into a shared balance.
func SetScope(provider, account string, scope Scope) {
	mu.Lock()
	defer mu.Unlock()
	k := accountKey{normalize(provider), strings.TrimSpace(account)}
	if k.provider == "" || k.account == "" {
		return
	}
	scope.Project, scope.Organization = strings.TrimSpace(scope.Project), strings.TrimSpace(scope.Organization)
	if scopes[k] != scope {
		// Do not revive old account-local hints if an account changes scope.
		for q := range quotas {
			if q.provider == k.provider && q.account == k.account {
				delete(quotas, q)
			}
		}
		for q := range blocks {
			if q.provider == k.provider && q.account == k.account {
				delete(blocks, q)
			}
		}
	}
	scopes[k] = scope
}

func Update(provider, account, model string, remaining float64, resetAt time.Time) {
	if normalize(provider) == "" || strings.TrimSpace(account) == "" || strings.TrimSpace(model) == "" {
		return
	}
	// Invalid telemetry must not become zero, unlimited or a routable balance.
	if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 || remaining > 100 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	quotas[quotaKey(provider, account, model)] = AccountQuota{remaining, resetAt.UTC(), time.Now().UTC()}
}

// ObserveCooldown never changes an official quota observation. A concurrent
// success or positive refresh cannot shorten this independent retry delay.
func ObserveCooldown(provider, account, model string, delay time.Duration) {
	if delay <= 0 || provider == "" || account == "" || model == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	k := quotaKey(provider, account, model)
	until := time.Now().Add(delay)
	if until.After(blocks[k]) {
		blocks[k] = until
	}
}

func snapshotLocked(k key, now time.Time) Snapshot {
	out := Snapshot{Status: Unknown}
	if until := blocks[k]; now.Before(until) {
		out.BlockedUntil = until
	}
	q, ok := quotas[k]
	if !ok {
		return out
	}
	out.RemainingPercentage, out.UpdatedAt, out.ResetAt = q.RemainingPercentage, q.UpdatedAt, q.ResetAt
	if now.Sub(q.UpdatedAt) >= MaxAge || (!q.ResetAt.IsZero() && !now.Before(q.ResetAt)) {
		out.Status = Stale // elapsed reset does not prove replenishment
	} else if q.RemainingPercentage <= 0 {
		out.Status = Exhausted // zero with unknown reset remains zero until stale
	} else {
		out.Status = Available
	}
	return out
}

func Inspect(provider, account, model string) Snapshot {
	mu.RLock()
	defer mu.RUnlock()
	return snapshotLocked(quotaKey(provider, account, model), time.Now())
}

func Get(provider, account, model string) (AccountQuota, bool) {
	s := Inspect(provider, account, model)
	return AccountQuota{s.RemainingPercentage, s.ResetAt, s.UpdatedAt}, s.Status == Available || s.Status == Exhausted
}

func IsExhausted(provider, account, model string) bool {
	return Inspect(provider, account, model).Status == Exhausted
}
func Eligible(s Snapshot) bool { return s.Status != Exhausted && s.BlockedUntil.IsZero() }

// OrderAccounts takes one locked snapshot before sorting. Unknown/stale are
// bounded fallbacks, never a claim of unlimited quota. Ties preserve priority.
func OrderAccounts(provider, model string, ids []string) []string {
	out := append([]string(nil), ids...)
	views := make(map[string]Snapshot, len(ids))
	mu.RLock()
	now := time.Now()
	for _, id := range ids {
		views[id] = snapshotLocked(quotaKey(provider, id, model), now)
	}
	mu.RUnlock()
	rank := func(s Snapshot) int {
		if !Eligible(s) {
			return 0
		}
		if s.Status == Available {
			return 2
		}
		return 1
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := views[out[i]], views[out[j]]
		if rank(a) != rank(b) {
			return rank(a) > rank(b)
		}
		return rank(a) == 2 && a.RemainingPercentage > b.RemainingPercentage
	})
	return out
}

func ClearProvider(provider string) {
	mu.Lock()
	defer mu.Unlock()
	p := normalize(provider)
	for k := range quotas {
		if k.provider == p {
			delete(quotas, k)
		}
	}
	for k := range blocks {
		if k.provider == p {
			delete(blocks, k)
		}
	}
	for k := range scopes {
		if k.provider == p {
			delete(scopes, k)
		}
	}
}

func ClearAll() {
	mu.Lock()
	defer mu.Unlock()
	quotas, blocks, scopes = map[key]AccountQuota{}, map[key]time.Time{}, map[accountKey]Scope{}
}
