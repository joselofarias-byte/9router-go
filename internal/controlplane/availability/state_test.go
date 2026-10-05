package availability

import (
	"sync"
	"testing"
	"time"
)

func TestCooldownIsolationExpiryAndConcurrentSuccess(t *testing.T) {
	s := NewStore()
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	key := Key{"provider", "model", "account"}
	s.Observe(key, false, "quota", time.Minute, 0, 0)
	s.Observe(key, true, "", 0, 100, 40)
	if s.Available(key) {
		t.Fatal("a concurrent success cleared the quota block")
	}
	if !s.Available(Key{"provider", "other", "account"}) || !s.Available(Key{"provider", "model", "other"}) {
		t.Fatal("block escaped its model/account")
	}
	now = now.Add(time.Minute)
	if !s.Available(key) {
		t.Fatal("expired cooldown blocks recovery")
	}
	if v := s.Get(key); v.Attempts != 2 || v.Successes != 1 || v.LatencyMs != 100 || v.TTFTMs != 40 {
		t.Fatalf("unexpected observations: %+v", v)
	}
}

func TestConcurrentObservations(t *testing.T) {
	s := NewStore()
	key := Key{"provider", "model", "account"}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { s.Observe(key, true, "", 0, 10, 4); _ = s.Get(key) })
	}
	wg.Wait()
	if v := s.Get(key); v.Attempts != 50 || v.Successes != 50 {
		t.Fatalf("lost updates: %+v", v)
	}
}
