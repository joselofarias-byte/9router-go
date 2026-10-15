package agentsupervisor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCompletedAndValidated(t *testing.T) {
	r := (Supervisor{Timeout: time.Second}).Run(context.Background(), func(context.Context) error { return nil }, func(context.Context) error { return nil })
	if r.Status != Completed || r.Validation != Passed {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Duration < 0 || r.FinishedAt.Before(r.StartedAt) {
		t.Fatal("invalid timestamps")
	}
}
func TestCompletedButValidationFailed(t *testing.T) {
	r := (Supervisor{}).Run(context.Background(), func(context.Context) error { return nil }, func(context.Context) error { return errors.New("tests failed") })
	if r.Status != Completed || r.Validation != Rejected {
		t.Fatalf("unexpected result: %+v", r)
	}
}
func TestRunnerError(t *testing.T) {
	r := (Supervisor{}).Run(context.Background(), func(context.Context) error { return errors.New("broken") }, nil)
	if r.Status != Failed || r.Validation != NotChecked {
		t.Fatalf("unexpected result: %+v", r)
	}
}
func TestTimeout(t *testing.T) {
	r := (Supervisor{Timeout: time.Millisecond}).Run(context.Background(), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, nil)
	if r.Status != TimedOut {
		t.Fatalf("unexpected result: %+v", r)
	}
}
func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := (Supervisor{}).Run(ctx, func(ctx context.Context) error { return ctx.Err() }, nil)
	if r.Status != Cancelled {
		t.Fatalf("unexpected result: %+v", r)
	}
}
func TestNilRunner(t *testing.T) {
	r := (Supervisor{}).Run(context.Background(), nil, nil)
	if r.Status != Failed {
		t.Fatalf("unexpected result: %+v", r)
	}
}
