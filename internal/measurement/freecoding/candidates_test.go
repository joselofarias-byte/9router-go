package freecoding

import (
	"testing"
	"time"

	"9router/proxy/internal/controlplane/discovery"
)

func TestCollectTrialsDropsPaidAndSorts(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := CollectTrials([]discovery.Candidate{
		{ProviderID: "openrouter", ModelID: "b:free", PricingMode: "free_tier", Capabilities: `{"tools":false}`},
		{ProviderID: "openrouter", ModelID: "c:free", PricingMode: "paid", Capabilities: `{"tools":true}`},
		{ProviderID: "openrouter", ModelID: "a:free", PricingMode: "free_tier", Capabilities: `{"tools":true}`},
		{ProviderID: "cline", ModelID: "d", PricingMode: "free", Capabilities: `{}`},
	}, TierOffline, "fixture:test", now)
	if len(got) != 3 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].ModelID != "d" || got[1].ModelID != "a:free" || got[2].ModelID != "b:free" {
		t.Fatalf("order: %+v", got)
	}
	if !got[1].CodingProfileEligible || got[1].ToolsDeclared == nil || !*got[1].ToolsDeclared {
		t.Fatalf("eligible: %+v", got[1])
	}
	if got[2].CodingProfileEligible || got[2].ToolsDeclared == nil || *got[2].ToolsDeclared {
		t.Fatalf("tools false: %+v", got[2])
	}
	if got[0].CodingProfileEligible || got[0].ToolsDeclared != nil {
		t.Fatalf("undeclared tools: %+v", got[0])
	}
	allow := LoadAllowlist(got)
	if !allow.Has("openrouter", "a:free") || allow.Has("openrouter", "c:free") {
		t.Fatal("allowlist")
	}
}

func TestDiscoverFixturesCandidateSet(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	recs, err := DiscoverFixtures(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, rec := range recs {
		if err := rec.Validate(); err != nil {
			t.Fatal(err)
		}
		if rec.RecordType != RecordTrial {
			continue
		}
		ids = append(ids, rec.Provider+"/"+rec.ModelID)
		switch rec.ModelID {
		case "qwen/coder:free":
			if rec.PricingMode != "free_tier" || !rec.CodingProfileEligible || rec.Tier != TierOffline {
				t.Fatalf("qwen: %+v", rec)
			}
		case "cline-free/muse-spark-1.3-contributor", "deepseek/deepseek-v4-flash":
			if rec.PricingMode != "free" || rec.CodingProfileEligible || rec.Provider != "cline" {
				t.Fatalf("cline: %+v", rec)
			}
		default:
			t.Fatalf("unexpected trial %s", rec.ModelID)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("trials: %v", ids)
	}
	for _, banned := range []string{"vendor/mislabeled:free", "vendor/paid-model", "openai/gpt-6-astra", "cline-pass/deepseek-v4-pro"} {
		for _, id := range ids {
			if id == "openrouter/"+banned || id == "cline/"+banned {
				t.Fatalf("paid or non-free row kept: %s", id)
			}
		}
	}
}
