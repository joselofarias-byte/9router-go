// Package availability tracks observed route health. Cooldowns are local
// retry delays. They are not a claim about a provider's remaining quota.
package availability

import (
	"sync"
	"time"
)

// Key isolates observations by provider, catalog model, and account.
type Key struct{ Provider, Model, Account string }

// State is the in-memory health of one key.
type State struct {
	Attempts     uint64    `json:"attempts"`
	Successes    uint64    `json:"successes"`
	LatencyMs    float64   `json:"latencyMs"`
	TTFTMs       float64   `json:"ttftMs"`
	BlockedUntil time.Time `json:"blockedUntil"`
	Reason       string    `json:"reason,omitempty"`
}

// Store is safe for concurrent Observe and Get.
type Store struct {
	mu      sync.RWMutex
	records map[Key]State
	now     func() time.Time
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{records: map[Key]State{}, now: time.Now} }

// Observe records one meaningful upstream outcome. A success does not clear
// another request's rate-limit block. cooldown is a retry delay, not a balance.
func (s *Store) Observe(key Key, success bool, reason string, cooldown time.Duration, latencyMs, ttftMs int) {
	if s == nil {
		return
	}
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

// Get returns a copy. An expired cooldown is cleared on the copy only.
func (s *Store) Get(key Key) State {
	if s == nil {
		return State{}
	}
	s.mu.RLock()
	v := s.records[key]
	s.mu.RUnlock()
	if !s.now().Before(v.BlockedUntil) {
		v.BlockedUntil, v.Reason = time.Time{}, ""
	}
	return v
}

// Available reports that the key is not inside an observed cooldown.
func (s *Store) Available(key Key) bool { return s.Get(key).BlockedUntil.IsZero() }
