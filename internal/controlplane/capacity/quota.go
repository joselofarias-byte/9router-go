// Package capacity holds provider-reported quota separately from observed
// retry delays. A 429, a retryAfter, or a strike block never becomes a balance.
// Snapshots carry no credentials.
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

// Scope is explicit provider metadata. It is never guessed from an account name
// and is not copied into routing diagnostics.
type Scope struct{ Project, Organization string }

type accountKey struct{ provider, account string }

type key struct {
	provider, account, model string
	scope                    Scope
}

// Snapshot is the routing view of one provider/account/model observation.
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

// SetScope associates accounts with one shared limit only when the caller
// already knows the project or organization. It does not copy or sum
// account-local observations into that shared balance.
func SetScope(provider, account string, scope Scope) {
	mu.Lock()
	defer mu.Unlock()
	k := accountKey{normalize(provider), strings.TrimSpace(account)}
	if k.provider == "" || k.account == "" {
		return
	}
	scope.Project, scope.Organization = strings.TrimSpace(scope.Project), strings.TrimSpace(scope.Organization)
	if scopes[k] != scope {
		dropAccountLocked(k)
	}
	scopes[k] = scope
}

func dropAccountLocked(k accountKey) {
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

// Update records a provider-reported remaining percentage in [0,100].
// NaN, infinity, and out-of-range values are ignored so they cannot become
// zero, unlimited, or a routable balance.
func Update(provider, account, model string, remaining float64, resetAt time.Time) {
	if normalize(provider) == "" || strings.TrimSpace(account) == "" || strings.TrimSpace(model) == "" {
		return
	}
	if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 || remaining > 100 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	quotas[quotaKey(provider, account, model)] = AccountQuota{remaining, resetAt.UTC(), time.Now().UTC()}
}

// ObserveCooldown records a local retry delay. It does not change any
// reported remaining percentage, and a later refresh cannot shorten it.
func ObserveCooldown(provider, account, model string, delay time.Duration) {
	if delay <= 0 || strings.TrimSpace(provider) == "" || strings.TrimSpace(account) == "" || strings.TrimSpace(model) == "" {
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
	switch {
	case now.Sub(q.UpdatedAt) >= MaxAge || (!q.ResetAt.IsZero() && !now.Before(q.ResetAt)):
		out.Status = Stale
	case q.RemainingPercentage <= 0:
		out.Status = Exhausted
	default:
		out.Status = Available
	}
	return out
}

// Inspect returns the current routing view. An elapsed reset becomes stale;
// it does not prove the balance was replenished.
func Inspect(provider, account, model string) Snapshot {
	mu.RLock()
	defer mu.RUnlock()
	return snapshotLocked(quotaKey(provider, account, model), time.Now())
}

// Get returns a fresh reported observation. Unknown and stale views are not a balance.
func Get(provider, account, model string) (AccountQuota, bool) {
	s := Inspect(provider, account, model)
	return AccountQuota{s.RemainingPercentage, s.ResetAt, s.UpdatedAt}, s.Status == Available || s.Status == Exhausted
}

// IsExhausted reports a fresh zero-remaining observation.
func IsExhausted(provider, account, model string) bool {
	return Inspect(provider, account, model).Status == Exhausted
}

// Eligible is false for a fresh zero balance or an unexpired observed cooldown.
// Unknown and stale remain eligible fallbacks; they are not treated as unlimited.
func Eligible(s Snapshot) bool { return s.Status != Exhausted && s.BlockedUntil.IsZero() }

// OrderAccounts ranks a locked snapshot. Fresh positive observations come
// first, unknown and stale stay in the caller's order, and exhausted or
// cooling accounts go last. Ties preserve priority.
func OrderAccounts(provider, model string, ids []string) []string {
	out := append([]string(nil), ids...)
	views := make(map[string]Snapshot, len(ids))
	mu.RLock()
	now := time.Now()
	for _, id := range ids {
		views[id] = snapshotLocked(quotaKey(provider, id, model), now)
	}
	mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		return betterAccount(views[out[i]], views[out[j]])
	})
	return out
}

func betterAccount(a, b Snapshot) bool {
	ar, br := accountRank(a), accountRank(b)
	if ar != br {
		return ar > br
	}
	return ar == 2 && a.RemainingPercentage > b.RemainingPercentage
}

func accountRank(s Snapshot) int {
	if !Eligible(s) {
		return 0
	}
	if s.Status == Available {
		return 2
	}
	return 1
}

// ClearProvider drops routing hints for one provider.
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

// ClearAll drops every in-memory hint. Tests use it to isolate global state.
func ClearAll() {
	mu.Lock()
	defer mu.Unlock()
	quotas, blocks, scopes = map[key]AccountQuota{}, map[key]time.Time{}, map[accountKey]Scope{}
}
