// Package agentsupervisor supervises agent execution without changing routing.
package agentsupervisor

import (
	"context"
	"errors"
	"time"
)

type Status string

const (
	Completed Status = "completed"
	Failed    Status = "failed"
	TimedOut  Status = "timed_out"
	Cancelled Status = "cancelled"
)

type Validation string

const (
	NotChecked Validation = "not_checked"
	Passed     Validation = "passed"
	Rejected   Validation = "failed"
)

type Result struct {
	Status     Status        `json:"status"`
	Validation Validation    `json:"validation"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	Duration   time.Duration `json:"duration_ns"`
	Error      string        `json:"error,omitempty"`
}

type Runner func(context.Context) error
type Validator func(context.Context) error

type Supervisor struct{ Timeout time.Duration }

// Run executes a cooperative runner and validates only after successful completion.
// Runner implementations must honor context cancellation; Run never abandons a goroutine.
func (s Supervisor) Run(ctx context.Context, run Runner, validate Validator) Result {
	start := time.Now()
	result := Result{StartedAt: start, Validation: NotChecked}
	if run == nil {
		result.Status = Failed
		result.Error = "nil runner"
		return finish(result)
	}
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	err := run(ctx)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Status = TimedOut
	case errors.Is(ctx.Err(), context.Canceled):
		result.Status = Cancelled
	case err != nil:
		result.Status = Failed
	default:
		result.Status = Completed
	}
	if err != nil {
		result.Error = err.Error()
	} else if ctx.Err() != nil {
		result.Error = ctx.Err().Error()
	}
	if result.Status == Completed && validate != nil {
		if validationErr := validate(ctx); validationErr != nil {
			result.Validation = Rejected
			result.Error = validationErr.Error()
		} else {
			result.Validation = Passed
		}
	}
	return finish(result)
}
func finish(r Result) Result {
	r.FinishedAt = time.Now()
	r.Duration = r.FinishedAt.Sub(r.StartedAt)
	return r
}
