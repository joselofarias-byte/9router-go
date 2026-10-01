package freecoding

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseJSONLRoundTripAndRejects(t *testing.T) {
	recs := []Record{validTaskRun()}
	var buf bytes.Buffer
	if err := WriteJSONL(&buf, recs); err != nil {
		t.Fatal(err)
	}
	got, err := ParseJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Provider != "openrouter" || got[0].ModelID != "qwen/coder:free" || !got[0].Pass {
		t.Fatalf("round trip: %+v", got)
	}

	bad := []string{
		`{"record_type":"task_run","utc":"not-a-time","tier":"authenticated_inference","provider":"openrouter","model_id":"qwen/coder:free","task":"bugfix","run":1,"quota_scope":"unknown","provenance":"x"}`,
		`{"record_type":"task_run","utc":"2026-10-01T00:00:00Z","tier":"marketing","provider":"openrouter","model_id":"qwen/coder:free","task":"bugfix","run":1,"quota_scope":"unknown","provenance":"x"}`,
		`{"record_type":"task_run","utc":"2026-10-01T00:00:00Z","tier":"authenticated_inference","provider":"openrouter","model_id":"","task":"bugfix","run":1,"quota_scope":"unknown","provenance":"x"}`,
		`{"record_type":"trial_candidate","utc":"2026-10-01T00:00:00Z","tier":"authenticated_inference","provider":"openrouter","model_id":"qwen/coder:free","pricing_mode":"free_tier","quota_scope":"not_applicable","provenance":"x","note":"n"}`,
		`{"record_type":"inference_skip","utc":"2026-10-01T00:00:00Z","tier":"authenticated_inference","skip":false,"quota_scope":"not_applicable","provenance":"x"}`,
	}
	for _, line := range bad {
		if _, err := ParseJSONL(strings.NewReader(line + "\n")); err == nil {
			t.Fatalf("accepted invalid row: %s", line)
		}
	}
}

func validTaskRun() Record {
	return Record{
		RecordType: RecordTaskRun,
		UTC:        "2026-10-01T00:00:00Z",
		Tier:       TierInference,
		Provider:   "openrouter",
		ModelID:    "qwen/coder:free",
		Task:       TaskBugfix,
		Run:        1,
		Pass:       true,
		HTTPStatus: 200,
		LatencyMs:  10,
		QuotaScope: QuotaUnknown,
		Provenance: openRouterChatURL,
	}
}
