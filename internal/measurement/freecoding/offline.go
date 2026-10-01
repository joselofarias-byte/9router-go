package freecoding

import (
	"context"
	"fmt"
	"time"
)

const offlineProvenance = "embed:testdata self-check; not a provider measurement"

// OfflineRecords grades the embedded reference submissions and classifies
// the embedded OpenRouter and Cline catalog fixtures. It does not call a
// network API and it does not rank models.
func OfflineRecords(ctx context.Context, now time.Time) ([]Record, error) {
	now = now.UTC()
	checks := []struct {
		model string
		task  string
		file  string
	}{
		{"fixture/reference-pass", TaskBugfix, "testdata/bugfix/pass/sum.go"},
		{"fixture/reference-pass", TaskRefactor, "testdata/refactor/pass/stats.go"},
		{"fixture/reference-pass", TaskReview, "testdata/review/pass.txt"},
		{"fixture/reference-fail", TaskBugfix, "testdata/bugfix/fail/sum.go"},
		{"fixture/reference-fail", TaskRefactor, "testdata/refactor/fail/stats.go"},
		{"fixture/reference-fail", TaskReview, "testdata/review/fail.txt"},
	}
	var out []Record
	for _, check := range checks {
		body, err := fixtureFS.ReadFile(check.file)
		if err != nil {
			return nil, err
		}
		start := time.Now()
		graded, err := Grade(check.task, string(body))
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", check.model, check.task, err)
		}
		out = append(out, Record{
			RecordType:     RecordTaskRun,
			UTC:            now.Format(time.RFC3339),
			Tier:           TierOffline,
			Provider:       "fixture",
			ModelID:        check.model,
			Task:           check.task,
			Run:            1,
			Pass:           graded.Pass,
			HTTPStatus:     0,
			LatencyMs:      time.Since(start).Milliseconds(),
			Status429Count: 0,
			CompileOK:      graded.CompileOK,
			TestOK:         graded.TestOK,
			ReviewMatch:    graded.ReviewMatch,
			QuotaScope:     QuotaNone,
			Provenance:     offlineProvenance,
			Note:           "harness self-check; not a provider measurement",
			Error:          graded.Error,
		})
	}
	discovered, err := DiscoverFixtures(ctx, now)
	if err != nil {
		return nil, err
	}
	out = append(out, discovered...)
	return out, nil
}
