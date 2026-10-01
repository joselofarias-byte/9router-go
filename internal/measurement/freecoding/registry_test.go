package freecoding

import "testing"

func TestRankingRequiresComparableAuthenticatedRuns(t *testing.T) {
	base := comparableRuns()
	reg := NewRegistry(base)
	rank := reg.Ranking()
	if !rank.Allowed || len(rank.Order) != 2 {
		t.Fatalf("ranking: %+v", rank)
	}
	if rank.Order[0].ModelID != "fast:free" || rank.Order[1].ModelID != "slow:free" {
		t.Fatalf("order: %+v", rank.Order)
	}
	if rank.Order[0].Passes != 3 || rank.Order[0].MeanLatencyMs != 10 {
		t.Fatalf("winner: %+v", rank.Order[0])
	}

	mixed := append([]Record{}, base...)
	offline := validTaskRun()
	offline.Tier = TierOffline
	offline.Provider = "fixture"
	offline.ModelID = "fixture/reference-pass"
	offline.QuotaScope = QuotaNone
	offline.Provenance = offlineProvenance
	mixed = append(mixed, offline)
	if got := NewRegistry(mixed).Ranking(); got.Allowed {
		t.Fatal("fixture row unlocked a ranking")
	}

	skipped := append([]Record{}, base...)
	skipped[0].Skip = true
	skipped[0].SkipReason = "interrupted"
	if got := NewRegistry(skipped).Ranking(); got.Allowed {
		t.Fatal("skip unlocked a ranking")
	}

	if got := NewRegistry(base[:3]).Ranking(); got.Allowed {
		t.Fatal("single model unlocked a ranking")
	}

	card := NewRegistry(mixed).Scorecard()
	if len(card) < 2 || card[0].Provider > card[len(card)-1].Provider && card[0].Tier == card[len(card)-1].Tier {
		t.Fatalf("scorecard: %+v", card)
	}
	for i := 1; i < len(card); i++ {
		if scoreLessAlpha(card[i], card[i-1]) {
			t.Fatalf("scorecard not alphabetical: %+v", card)
		}
	}
}

func comparableRuns() []Record {
	var out []Record
	for _, spec := range []struct {
		model string
		pass  bool
		lat   int64
	}{
		{"slow:free", false, 50},
		{"fast:free", true, 10},
	} {
		for _, task := range []string{TaskBugfix, TaskRefactor, TaskReview} {
			rec := validTaskRun()
			rec.ModelID = spec.model
			rec.Task = task
			rec.Pass = spec.pass || task == TaskReview
			rec.LatencyMs = spec.lat
			if spec.model == "slow:free" && task == TaskBugfix {
				rec.Pass = false
			}
			if spec.model == "fast:free" {
				rec.Pass = true
			}
			out = append(out, rec)
		}
	}
	return out
}
