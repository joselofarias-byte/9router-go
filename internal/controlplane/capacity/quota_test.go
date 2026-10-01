package capacity

import (
	"reflect"
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
