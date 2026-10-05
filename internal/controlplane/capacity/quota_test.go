package capacity

import (
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestOrderAccountsByRemainingQuota(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)

	reset := time.Now().UTC().Add(time.Hour)
	Update("antigravity", "a", "model-x", 25, reset)
	Update("antigravity", "b", "model-x", 80, reset)
	Update("antigravity", "c", "model-x", 0, reset)

	got := OrderAccounts("antigravity", "model-x", []string{"a", "c", "unknown", "b"})
	want := []string{"b", "a", "unknown", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OrderAccounts()=%v want %v", got, want)
	}
}

func TestQuotaIsScopedByProviderAccountAndModel(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)

	reset := time.Now().UTC().Add(time.Hour)
	Update("antigravity", "acct-1", "gemini-x", 40, reset)

	if _, ok := Get("antigravity", "acct-1", "gemini-x"); !ok {
		t.Fatal("expected exact quota observation")
	}
	if _, ok := Get("antigravity", "acct-2", "gemini-x"); ok {
		t.Fatal("quota leaked across accounts")
	}
	if _, ok := Get("codex", "acct-1", "gemini-x"); ok {
		t.Fatal("quota leaked across providers")
	}
	if _, ok := Get("antigravity", "acct-1", "claude-x"); ok {
		t.Fatal("quota leaked across models")
	}
}

func TestExhaustedOnlyWhileResetIsFuture(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)

	Update("antigravity", "acct", "model", 0, time.Now().UTC().Add(time.Minute))
	if !IsExhausted("antigravity", "acct", "model") {
		t.Fatal("expected future zero quota to be exhausted")
	}

	Update("antigravity", "acct", "model", 0, time.Now().UTC().Add(-time.Minute))
	if IsExhausted("antigravity", "acct", "model") {
		t.Fatal("past reset must not remain exhausted")
	}
}

func TestFreshnessResetAndCooldownRecovery(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)
	now := time.Now()
	Update("p", "a", "m", 0, time.Time{})
	if Inspect("p", "a", "m").Status != Exhausted {
		t.Fatal("fresh zero without reset is exhausted")
	}
	mu.Lock()
	k := quotaKey("p", "a", "m")
	q := quotas[k]
	q.UpdatedAt = now.Add(-MaxAge)
	quotas[k] = q
	mu.Unlock()
	if Inspect("p", "a", "m").Status != Stale {
		t.Fatal("expired observation is stale")
	}
	Update("p", "a", "m", 90, now.Add(-time.Minute))
	if Inspect("p", "a", "m").Status != Stale {
		t.Fatal("elapsed reset cannot prove remaining balance")
	}
	Update("p", "a", "m", 90, now.Add(time.Hour))
	ObserveCooldown("p", "a", "m", time.Minute)
	Update("p", "a", "m", 100, now.Add(time.Hour))
	if Eligible(Inspect("p", "a", "m")) {
		t.Fatal("refresh erased cooldown")
	}
	if q, ok := Get("p", "a", "m"); !ok || q.RemainingPercentage != 100 {
		t.Fatal("cooldown fabricated quota")
	}
	mu.Lock()
	blocks[quotaKey("p", "a", "m")] = now.Add(-time.Second)
	mu.Unlock()
	if !Eligible(Inspect("p", "a", "m")) {
		t.Fatal("expired cooldown did not recover")
	}
}

func TestSharedScopeNeverAddsQuotasAndSharesObservedBlocks(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)
	scope := Scope{Project: "fixture-project", Organization: "fixture-org"}
	SetScope("p", "a", scope)
	SetScope("p", "b", scope)
	SetScope("other", "b", scope)
	SetScope("p", "other-project", Scope{Project: "separate"})
	Update("p", "a", "m", 60, time.Now().Add(time.Hour))
	Update("p", "b", "m", 40, time.Now().Add(time.Hour))
	if q, ok := Get("p", "a", "m"); !ok || q.RemainingPercentage != 40 {
		t.Fatal("shared quota added or duplicated")
	}
	ObserveCooldown("p", "a", "m", time.Minute)
	if Eligible(Inspect("p", "b", "m")) {
		t.Fatal("shared scope bypassed by sibling")
	}
	for _, tc := range []struct{ p, a, m string }{{"other", "b", "m"}, {"p", "other-project", "m"}, {"p", "b", "different"}} {
		if s := Inspect(tc.p, tc.a, tc.m); s.Status != Unknown || !Eligible(s) {
			t.Fatal("scope leaked across provider/project/model")
		}
	}
	SetScope("p", "a", Scope{Project: "new"})
	if Inspect("p", "a", "m").Status != Unknown {
		t.Fatal("old project quota reused")
	}
}

func TestConcurrentUpdatesAndSorting(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				Update("p", "a", "m", float64(j), time.Now().Add(time.Hour))
				Update("p", "b", "m", float64(100-j), time.Now().Add(time.Hour))
				if got := OrderAccounts("p", "m", []string{"a", "b", "unknown"}); len(got) != 3 {
					t.Errorf("lost accounts: %v", got)
				}
			}
		}()
	}
	wg.Wait()
}

func TestInvalidTelemetryIsUnknown(t *testing.T) {
	ClearAll()
	t.Cleanup(ClearAll)
	for _, v := range []float64{math.NaN(), math.Inf(1), -1, 101} {
		Update("p", "a", "m", v, time.Time{})
	}
	if Inspect("p", "a", "m").Status != Unknown {
		t.Fatal("invalid telemetry became a balance")
	}
}
