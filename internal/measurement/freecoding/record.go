// Package freecoding records coding measurements for free providers.
// Catalog rows, fixture self-checks, and authenticated inference stay in
// separate tiers so a model list is never presented as a benchmark ranking.
package freecoding

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	TierOffline   = "offline_fixture"
	TierDiscovery = "public_discovery"
	TierInference = "authenticated_inference"

	RecordTaskRun   = "task_run"
	RecordCatalog   = "catalog_observation"
	RecordTrial     = "trial_candidate"
	RecordInferSkip = "inference_skip"

	TaskBugfix   = "bugfix"
	TaskRefactor = "refactor"
	TaskReview   = "review"

	QuotaNone          = "none"
	QuotaNotApplicable = "not_applicable"
	QuotaUnknown       = "unknown"
)

// Record is one JSONL row. Task runs, catalog observations, trial
// candidates, and inference skips share a file and are distinguished by
// record_type.
type Record struct {
	RecordType            string `json:"record_type"`
	UTC                   string `json:"utc"`
	Tier                  string `json:"tier"`
	Provider              string `json:"provider,omitempty"`
	ModelID               string `json:"model_id,omitempty"`
	Task                  string `json:"task,omitempty"`
	Run                   int    `json:"run,omitempty"`
	Pass                  bool   `json:"pass"`
	Skip                  bool   `json:"skip"`
	SkipReason            string `json:"skip_reason,omitempty"`
	HTTPStatus            int    `json:"http_status"`
	LatencyMs             int64  `json:"latency_ms"`
	Status429Count        int    `json:"status_429_count"`
	CompileOK             *bool  `json:"compile_ok,omitempty"`
	TestOK                *bool  `json:"test_ok,omitempty"`
	ReviewMatch           *bool  `json:"review_match,omitempty"`
	ReportedCost          string `json:"reported_cost,omitempty"`
	QuotaScope            string `json:"quota_scope"`
	Provenance            string `json:"provenance"`
	PricingMode           string `json:"pricing_mode,omitempty"`
	ToolsDeclared         *bool  `json:"tools_declared,omitempty"`
	CodingProfileEligible bool   `json:"coding_profile_eligible,omitempty"`
	CandidateCount        int    `json:"candidate_count,omitempty"`
	Error                 string `json:"error,omitempty"`
	Note                  string `json:"note,omitempty"`
}

func (r Record) Validate() error {
	if _, err := time.Parse(time.RFC3339, r.UTC); err != nil {
		return fmt.Errorf("utc: %w", err)
	}
	switch r.Tier {
	case TierOffline, TierDiscovery, TierInference:
	default:
		return fmt.Errorf("tier %q", r.Tier)
	}
	if strings.TrimSpace(r.Provenance) == "" {
		return fmt.Errorf("provenance required")
	}
	if strings.TrimSpace(r.QuotaScope) == "" {
		return fmt.Errorf("quota_scope required")
	}
	switch r.RecordType {
	case RecordTaskRun:
		if r.Provider == "" || r.ModelID == "" {
			return fmt.Errorf("task_run requires provider and model_id")
		}
		switch r.Task {
		case TaskBugfix, TaskRefactor, TaskReview:
		default:
			return fmt.Errorf("task %q", r.Task)
		}
		if r.Run < 1 {
			return fmt.Errorf("run must be >= 1")
		}
		if r.Skip && r.SkipReason == "" {
			return fmt.Errorf("skip_reason required when skip is set")
		}
	case RecordCatalog:
		if r.Provider == "" {
			return fmt.Errorf("catalog observation requires provider")
		}
	case RecordTrial:
		if r.Provider == "" || r.ModelID == "" {
			return fmt.Errorf("trial candidate requires provider and model_id")
		}
		if r.PricingMode != "free" && r.PricingMode != "free_tier" {
			return fmt.Errorf("trial candidate pricing_mode %q", r.PricingMode)
		}
		if r.Tier == TierInference {
			return fmt.Errorf("trial candidates are catalog or fixture rows, not inference")
		}
		if r.Note == "" {
			return fmt.Errorf("trial candidate note required")
		}
	case RecordInferSkip:
		if !r.Skip || r.SkipReason == "" {
			return fmt.Errorf("inference_skip requires skip and skip_reason")
		}
	default:
		return fmt.Errorf("record_type %q", r.RecordType)
	}
	return nil
}

// ParseJSONL reads measurement rows. Blank lines are ignored.
func ParseJSONL(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 2<<20)
	var out []Record
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if err := rec.Validate(); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// WriteJSONL emits one validated JSON object per line.
func WriteJSONL(w io.Writer, recs []Record) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i, rec := range recs {
		if err := rec.Validate(); err != nil {
			return fmt.Errorf("record %d: %w", i, err)
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
