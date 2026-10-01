package freecoding

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOfflineRecordsStayUnranked(t *testing.T) {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	recs, err := OfflineRecords(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteJSONL(&buf, recs); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if rank := NewRegistry(parsed).Ranking(); rank.Allowed {
		t.Fatalf("offline ranking: %+v", rank)
	}
	var passBug, failBug, failRefactor, failReview bool
	for _, rec := range parsed {
		if rec.RecordType != RecordTaskRun {
			continue
		}
		if rec.Tier != TierOffline || rec.Provider != "fixture" || rec.QuotaScope != QuotaNone || rec.HTTPStatus != 0 {
			t.Fatalf("task provenance: %+v", rec)
		}
		switch rec.ModelID + "/" + rec.Task {
		case "fixture/reference-pass/" + TaskBugfix:
			passBug = rec.Pass
		case "fixture/reference-fail/" + TaskBugfix:
			failBug = !rec.Pass && rec.CompileOK != nil && *rec.CompileOK && rec.TestOK != nil && !*rec.TestOK
		case "fixture/reference-fail/" + TaskRefactor:
			failRefactor = !rec.Pass && rec.TestOK != nil && *rec.TestOK
		case "fixture/reference-fail/" + TaskReview:
			failReview = !rec.Pass && rec.ReviewMatch != nil && !*rec.ReviewMatch
		}
	}
	if !passBug || !failBug || !failRefactor || !failReview {
		t.Fatalf("outcomes passBug=%v failBug=%v failRefactor=%v failReview=%v", passBug, failBug, failRefactor, failReview)
	}
	var report bytes.Buffer
	if err := WriteReport(&report, parsed); err != nil {
		t.Fatal(err)
	}
	text := report.String()
	for _, want := range []string{"Ranking withheld", "qwen/coder:free", "Qué no afirmar", "not a coding benchmark"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q", want)
		}
	}
	if strings.Contains(text, "| Order |") {
		t.Fatal("report published an order without authenticated comparison")
	}
}

func TestReportRanksOnlyComparableInference(t *testing.T) {
	var report bytes.Buffer
	if err := WriteReport(&report, comparableRuns()); err != nil {
		t.Fatal(err)
	}
	text := report.String()
	if !strings.Contains(text, "| Order |") || !strings.Contains(text, "Not a general provider ranking") {
		t.Fatal(text)
	}
	if strings.Contains(text, "Ranking withheld") {
		t.Fatal("withheld a comparable ranking")
	}
}

func TestLiveDiscoveryOptIn(t *testing.T) {
	if os.Getenv("FREE_CODING_DISCOVERY") != "1" {
		t.Skip("FREE_CODING_DISCOVERY is not 1; public discovery stays opt-in")
	}
	recs, err := DiscoverPublic(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if CatalogsFailed(recs) {
		t.Fatal("both public catalogs failed")
	}
}
