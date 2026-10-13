package entitlements

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLeaseProviderDoesNotReactivateAfterGraceExpiry(t *testing.T) {
	raw, keys, ctx, lease := signedLeaseFixture(t, nil)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewLeaseProvider(evaluation)
	current := ctx.Now
	provider.now = func() time.Time { return current }

	if !provider.Enabled(CapabilityAdvancedRouting) {
		t.Fatal("provider should start active")
	}

	current = lease.GraceUntil
	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("provider did not become terminal Community at grace expiry")
	}

	current = ctx.Now
	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("provider reactivated after clock moved backward")
	}
}

func TestLeaseProviderDoesNotReactivateAfterBuildExpiry(t *testing.T) {
	raw, keys, ctx, _ := signedLeaseFixture(t, nil)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.ProCapableUntil == nil {
		t.Fatal("fixture missing build expiry")
	}

	provider := NewLeaseProvider(evaluation)
	current := *evaluation.ProCapableUntil
	provider.now = func() time.Time { return current }

	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("provider did not become terminal Community at build expiry")
	}

	current = ctx.Now
	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("provider reactivated after build expiry")
	}
}

func TestLeaseProviderFailsClosedOnRuntimeClockRollback(t *testing.T) {
	raw, keys, ctx, _ := signedLeaseFixture(t, nil)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewLeaseProvider(evaluation)
	current := ctx.Now
	provider.now = func() time.Time { return current }

	if !provider.Enabled(CapabilityAdvancedRouting) {
		t.Fatal("provider should start active")
	}

	current = ctx.Now.Add(-time.Nanosecond)
	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("provider did not fail closed on runtime clock rollback")
	}

	current = ctx.Now.Add(time.Hour)
	if provider.Enabled(CapabilityAdvancedRouting) || provider.Status().Mode != "community" {
		t.Fatal("provider reactivated after rollback terminal state")
	}
}

func TestLeaseProviderConcurrentReadsRespectTerminalState(t *testing.T) {
	raw, keys, ctx, lease := signedLeaseFixture(t, nil)
	evaluation, err := VerifySignedLease(raw, keys, ctx)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewLeaseProvider(evaluation)

	var nanos atomic.Int64
	nanos.Store(ctx.Now.UnixNano())
	provider.now = func() time.Time {
		return time.Unix(0, nanos.Load()).UTC()
	}

	run := func(wantEnabled bool) {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan string, 64)
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					if got := provider.Enabled(CapabilityAdvancedRouting); got != wantEnabled {
						errs <- "Enabled returned unexpected state"
						return
					}
					mode := provider.Status().Mode
					if wantEnabled && mode != "licensed" {
						errs <- "Status was not licensed"
						return
					}
					if !wantEnabled && mode != "community" {
						errs <- "Status was not community"
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for msg := range errs {
			t.Fatal(msg)
		}
	}

	run(true)
	nanos.Store(lease.GraceUntil.UnixNano())
	run(false)
	nanos.Store(ctx.Now.UnixNano())
	run(false)
}
