package trust

import (
	"strings"
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

// StaleLatencyAfter is how long a probe/traffic latency sample remains
// usable for scoring. Older samples are treated as unknown so a previously
// fast route cannot stay at the top after going silent.
const StaleLatencyAfter = 15 * time.Minute

// Record tracks observations for a specific node (provider/account/model combination)
type Record struct {
	Level               TrustLevel
	ConsecutiveFailures int
	ConsecutiveSuccess  int
	LastQuarantineAt    time.Time
	QuarantineUntil     time.Time

	TotalSuccess  int
	TotalFailures int
	LastSuccessAt time.Time
	LastFailureAt time.Time

	TotalLatencyMs int64
	LatencySamples int
	LastLatencyAt  time.Time

	// QuotaUntil is set when the node reports quota/rate-limit exhaustion.
	// SelectCandidates skips the node until this instant.
	QuotaUntil time.Time
	// SessionExpired marks an account that failed authentication because
	// its session/token is no longer valid. Cleared on a later success.
	SessionExpired bool
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

func (m *Manager) key(provider, model, account string) string {
	return provider + "|" + model + "|" + account
}

func (m *Manager) getOrCreateLocked(provider, model, account string) *Record {
	k := m.key(provider, model, account)
	r, exists := m.records[k]
	if !exists {
		r = &Record{Level: TrustUnknown}
		m.records[k] = r
	}
	return r
}

// RecordObservation updates trust state based on a request outcome or probe.
func (m *Manager) RecordObservation(provider, model, account string, success bool, errCat providers.ErrorCategory) {
	k := m.key(provider, model, account)

	m.mu.Lock()
	defer m.mu.Unlock()

	r := m.getOrCreateLocked(provider, model, account)

	if success {
		r.ConsecutiveFailures = 0
		r.ConsecutiveSuccess++
		r.TotalSuccess++
		r.LastSuccessAt = time.Now()
		r.SessionExpired = false

		if r.Level == TrustUnknown || r.Level == TrustCandidate {
			r.Level = TrustVerified
		} else if r.Level == TrustVerified && r.ConsecutiveSuccess > 10 {
			r.Level = TrustTrusted
		} else if r.Level == TrustDegraded && r.ConsecutiveSuccess > 3 {
			r.Level = TrustVerified
		} else if r.Level == TrustQuarantined && time.Now().After(r.QuarantineUntil) {
			r.Level = TrustDegraded
		}
		return
	}

	r.ConsecutiveSuccess = 0
	r.ConsecutiveFailures++
	r.TotalFailures++
	r.LastFailureAt = time.Now()

	switch errCat {
	case providers.ErrAuth, providers.ErrSession:
		r.SessionExpired = true
		m.quarantine(r, 60*time.Minute)
		log.Warn("trust", "immediate quarantine", "node", k, "reason", string(errCat))
	case providers.ErrPermanent, providers.ErrModelNotFound:
		m.quarantine(r, 60*time.Minute)
		log.Warn("trust", "immediate quarantine", "node", k, "reason", string(errCat))
	case providers.ErrQuota, providers.ErrRateLimit:
		r.QuotaUntil = time.Now().Add(2 * time.Minute)
		if r.Level == TrustTrusted || r.Level == TrustVerified {
			r.Level = TrustDegraded
		}
	default:
		if r.ConsecutiveFailures > 5 {
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

// RecordQuota marks a node unavailable until cooldown elapses.
func (m *Manager) RecordQuota(provider, model, account string, cooldown time.Duration) {
	if cooldown <= 0 {
		cooldown = 2 * time.Minute
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.getOrCreateLocked(provider, model, account)
	r.QuotaUntil = time.Now().Add(cooldown)
	if r.Level == TrustTrusted || r.Level == TrustVerified {
		r.Level = TrustDegraded
	}
}

// RecordSessionExpired marks the account session as unusable.
func (m *Manager) RecordSessionExpired(provider, model, account string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.getOrCreateLocked(provider, model, account)
	r.SessionExpired = true
	m.quarantine(r, 60*time.Minute)
}

// NeutralSuccessRate is the prior assumed for a node with no recorded
// observations yet.
const NeutralSuccessRate = 0.7

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

func (m *Manager) RecordLatency(provider, model, account string, latencyMs int) {
	if latencyMs < 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.getOrCreateLocked(provider, model, account)
	r.TotalLatencyMs += int64(latencyMs)
	r.LatencySamples++
	r.LastLatencyAt = time.Now()
}

// LatencyStats returns the average observed latency (ms) when a recent
// sample exists. Stale samples are reported as missing so scoring stays
// neutral instead of trusting old TTFT.
func (m *Manager) LatencyStats(provider, model, account string) (avgMs int, hasData bool) {
	k := m.key(provider, model, account)
	m.mu.RLock()
	defer m.mu.RUnlock()

	r, exists := m.records[k]
	if !exists || r.LatencySamples == 0 {
		return 0, false
	}
	if !r.LastLatencyAt.IsZero() && time.Since(r.LastLatencyAt) > StaleLatencyAfter {
		return 0, false
	}
	return int(r.TotalLatencyMs / int64(r.LatencySamples)), true
}

// IsUnavailable reports quota cooldown or expired-session blocks that
// scoring should treat as unroutable even if trust is not quarantined.
func (m *Manager) IsUnavailable(provider, model, account string) (bool, string) {
	k := m.key(provider, model, account)
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, exists := m.records[k]
	if !exists {
		return false, ""
	}
	if r.SessionExpired {
		return true, "session_expired"
	}
	if !r.QuotaUntil.IsZero() && time.Now().Before(r.QuotaUntil) {
		return true, "quota_exhausted"
	}
	return false, ""
}

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
	QuotaUntil          time.Time
	SessionExpired      bool
}

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
			QuotaUntil:          r.QuotaUntil,
			SessionExpired:      r.SessionExpired,
		})
	}
	return out
}

func splitKey(k string) (provider, model, account string) {
	parts := strings.SplitN(k, "|", 3)
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
		if r.Level == TrustQuarantined && time.Now().After(r.QuarantineUntil) {
			return TrustDegraded
		}
		return r.Level
	}
	return TrustUnknown
}
