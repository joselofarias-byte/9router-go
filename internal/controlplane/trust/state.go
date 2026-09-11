package trust

import (
	"sync"
	"time"

	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
)

type TrustLevel string

const (
	TrustUnknown     TrustLevel = "unknown"
	TrustCandidate   TrustLevel = "candidate"
	TrustVerified    TrustLevel = "verified"
	TrustTrusted     TrustLevel = "trusted"
	TrustDegraded    TrustLevel = "degraded"
	TrustQuarantined TrustLevel = "quarantined"
	TrustDisabled    TrustLevel = "disabled"
)

// Record tracks observations for a specific node (provider/account/model combination)
type Record struct {
	Level               TrustLevel
	ConsecutiveFailures int
	ConsecutiveSuccess  int
	LastQuarantineAt    time.Time
	QuarantineUntil     time.Time

	// TotalSuccess/TotalFailures are lifetime counters (not reset on a single
	// success/failure like the Consecutive* fields) used to compute a real
	// observed success rate for scoring, so routing reflects actual behavior
	// instead of a permanent placeholder.
	TotalSuccess  int
	TotalFailures int
	LastSuccessAt time.Time
	LastFailureAt time.Time

	// TotalLatencyMs/LatencySamples accumulate probe-observed round-trip
	// latency so scoring can read a real average TTFT for this node instead
	// of the neutral placeholder used before any probe has run.
	TotalLatencyMs int64
	LatencySamples int
}

type Manager struct {
	mu      sync.RWMutex
	records map[string]*Record
}

func NewManager() *Manager {
	return &Manager{
		records: make(map[string]*Record),
	}
}

// key generates a unique key for tracking
func (m *Manager) key(provider, model, account string) string {
	return provider + "|" + model + "|" + account
}

// RecordObservation updates trust state based on a request outcome or probe
func (m *Manager) RecordObservation(provider, model, account string, success bool, errCat providers.ErrorCategory) {
	k := m.key(provider, model, account)

	m.mu.Lock()
	defer m.mu.Unlock()

	r, exists := m.records[k]
	if !exists {
		r = &Record{Level: TrustUnknown}
		m.records[k] = r
	}

	if success {
		r.ConsecutiveFailures = 0
		r.ConsecutiveSuccess++
		r.TotalSuccess++
		r.LastSuccessAt = time.Now()

		if r.Level == TrustUnknown || r.Level == TrustCandidate {
			r.Level = TrustVerified
		} else if r.Level == TrustVerified && r.ConsecutiveSuccess > 10 {
			r.Level = TrustTrusted
		} else if r.Level == TrustDegraded && r.ConsecutiveSuccess > 3 {
			r.Level = TrustVerified
		} else if r.Level == TrustQuarantined && time.Now().After(r.QuarantineUntil) {
			r.Level = TrustDegraded
		}
	} else {
		r.ConsecutiveSuccess = 0
		r.ConsecutiveFailures++
		r.TotalFailures++
		r.LastFailureAt = time.Now()

		// Immediately quarantine on authentication, permanent, or
		// model-not-found failures — a node that fails one of these on the
		// exact provider/model/account it was invoked with will not recover
		// on the next request, so waiting out the failure-count threshold
		// below would just waste real user requests on a known-bad route.
		if errCat == providers.ErrAuth || errCat == providers.ErrPermanent || errCat == providers.ErrModelNotFound {
			m.quarantine(r, 60*time.Minute)
			log.Warn("trust", "immediate quarantine", "node", k, "reason", string(errCat))
		} else if r.ConsecutiveFailures > 5 {
			m.quarantine(r, 5*time.Minute)
			log.Warn("trust", "repeated failures quarantine", "node", k, "failures", r.ConsecutiveFailures)
		} else if r.Level == TrustTrusted || r.Level == TrustVerified {
			r.Level = TrustDegraded
		}
	}
}

func (m *Manager) quarantine(r *Record, duration time.Duration) {
	r.Level = TrustQuarantined
	r.LastQuarantineAt = time.Now()
	r.QuarantineUntil = time.Now().Add(duration)
}

// NeutralSuccessRate is the prior assumed for a node with no recorded
// observations yet — optimistic enough that an unproven free route still
// gets a fair shot against long-lived proven ones, without letting untested
// routes permanently outscore ones with a real track record.
const NeutralSuccessRate = 0.7

// SuccessRate returns the real observed success rate for a node (0.0-1.0)
// computed from lifetime TotalSuccess/TotalFailures, and whether any
// observations exist yet. Callers should fall back to NeutralSuccessRate
// when hasData is false, rather than treating an untested node as a 0%
// success rate.
func (m *Manager) SuccessRate(provider, model, account string) (rate float64, hasData bool) {
	k := m.key(provider, model, account)

	m.mu.RLock()
	defer m.mu.RUnlock()

	r, exists := m.records[k]
	if !exists {
		return 0, false
	}
	total := r.TotalSuccess + r.TotalFailures
	if total == 0 {
		return 0, false
	}
	return float64(r.TotalSuccess) / float64(total), true
}

// RecordLatency stores an observed round-trip latency (ms) for a node,
// typically from a verification probe run outside the hot path. It never
// affects trust level or quarantine — only the running average consulted by
// scoring for the performance dimension.
func (m *Manager) RecordLatency(provider, model, account string, latencyMs int) {
	if latencyMs < 0 {
		return
	}
	k := m.key(provider, model, account)

	m.mu.Lock()
	defer m.mu.Unlock()

	r, exists := m.records[k]
	if !exists {
		r = &Record{Level: TrustUnknown}
		m.records[k] = r
	}
	r.TotalLatencyMs += int64(latencyMs)
	r.LatencySamples++
}

// LatencyStats returns the average observed latency (ms) for a node and
// whether any probe has recorded a sample yet. Callers should fall back to a
// neutral performance score when hasData is false.
func (m *Manager) LatencyStats(provider, model, account string) (avgMs int, hasData bool) {
	k := m.key(provider, model, account)

	m.mu.RLock()
	defer m.mu.RUnlock()

	r, exists := m.records[k]
	if !exists || r.LatencySamples == 0 {
		return 0, false
	}
	return int(r.TotalLatencyMs / int64(r.LatencySamples)), true
}

// RecordSnapshot is a read-only copy of a trust Record plus the node key it
// tracks, safe to expose over a status API without leaking mutable state.
type RecordSnapshot struct {
	Provider            string
	Model               string
	Account             string
	Level               TrustLevel
	ConsecutiveFailures int
	ConsecutiveSuccess  int
	TotalSuccess        int
	TotalFailures       int
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	QuarantineUntil     time.Time
}

// Snapshot returns a point-in-time copy of every tracked node's trust state,
// for status/observability endpoints. It never returns the live map so
// callers cannot mutate manager state.
func (m *Manager) Snapshot() []RecordSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]RecordSnapshot, 0, len(m.records))
	for k, r := range m.records {
		provider, model, account := splitKey(k)
		out = append(out, RecordSnapshot{
			Provider:            provider,
			Model:               model,
			Account:             account,
			Level:               r.Level,
			ConsecutiveFailures: r.ConsecutiveFailures,
			ConsecutiveSuccess:  r.ConsecutiveSuccess,
			TotalSuccess:        r.TotalSuccess,
			TotalFailures:       r.TotalFailures,
			LastSuccessAt:       r.LastSuccessAt,
			LastFailureAt:       r.LastFailureAt,
			QuarantineUntil:     r.QuarantineUntil,
		})
	}
	return out
}

// splitKey reverses Manager.key. Provider/model/account values themselves
// never contain "|" (they come from provider IDs and model slugs), so a
// simple split is safe.
func splitKey(k string) (provider, model, account string) {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			parts = append(parts, k[start:i])
			start = i + 1
		}
	}
	parts = append(parts, k[start:])
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2]
}

func (m *Manager) GetTrustLevel(provider, model, account string) TrustLevel {
	k := m.key(provider, model, account)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if r, exists := m.records[k]; exists {
		// Auto-recover if quarantine period expired
		if r.Level == TrustQuarantined && time.Now().After(r.QuarantineUntil) {
			return TrustDegraded
		}
		return r.Level
	}
	return TrustUnknown
}
