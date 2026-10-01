// Package availability tracks observed route health, never invented quota balances.
package availability

import (
	"sync"
	"time"
)

type Key struct{ Provider, Model, Account string }

type State struct {
	Attempts     uint64    `json:"attempts"`
	Successes    uint64    `json:"successes"`
	LatencyMs    float64   `json:"latencyMs"`
	TTFTMs       float64   `json:"ttftMs"`
	BlockedUntil time.Time `json:"blockedUntil"`
	Reason       string    `json:"reason,omitempty"`
}

type Store struct {
	mu      sync.RWMutex
	records map[Key]State
	now     func() time.Time
}

func NewStore() *Store { return &Store{records: map[Key]State{}, now: time.Now} }

// Observe is called only for meaningful upstream outcomes. cooldown is a local
// retry delay, not a claim about a provider's remaining daily/monthly quota.
func (s *Store) Observe(key Key, success bool, reason string, cooldown time.Duration, latencyMs, ttftMs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.records[key]
	v.Attempts++
	if success {
		v.Successes++
		if latencyMs > 0 {
			v.LatencyMs = ewma(v.LatencyMs, float64(latencyMs))
		}
		if ttftMs > 0 {
			v.TTFTMs = ewma(v.TTFTMs, float64(ttftMs))
		}
		// A concurrent success must not undo another request's rate-limit block.
	} else if cooldown > 0 {
		until := s.now().Add(cooldown)
		if until.After(v.BlockedUntil) {
			v.BlockedUntil, v.Reason = until, reason
		}
	}
	s.records[key] = v
}

func ewma(old, sample float64) float64 {
	if old == 0 {
		return sample
	}
	return old*.8 + sample*.2
}

func (s *Store) Get(key Key) State {
	s.mu.RLock()
	v := s.records[key]
	s.mu.RUnlock()
	if !s.now().Before(v.BlockedUntil) {
		v.BlockedUntil, v.Reason = time.Time{}, ""
	}
	return v
}

func (s *Store) Available(key Key) bool { return s.Get(key).BlockedUntil.IsZero() }
