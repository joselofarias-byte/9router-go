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

		// Immediately quarantine on authentication or permanent failures
		if errCat == providers.ErrAuth || errCat == providers.ErrPermanent {
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
