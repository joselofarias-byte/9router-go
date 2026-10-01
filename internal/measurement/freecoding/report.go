package freecoding

import (
	"fmt"
	"io"
	"strings"
)

// WriteReport renders a Markdown report. A quality order is included only
// when Ranking.Allowed is true. Fixture and catalog sections stay labeled
// as non-rankings.
func WriteReport(w io.Writer, recs []Record) error {
	reg := NewRegistry(recs)
	rank := reg.Ranking()
	var b strings.Builder
	b.WriteString("# Free-coding measurement\n\n")
	b.WriteString("This report separates three tiers: `offline_fixture`, `public_discovery`, and `authenticated_inference`.\n")
	b.WriteString("Catalog eligibility and fixture self-checks are not a coding benchmark.\n\n")
	b.WriteString("## Task runs\n\n")
	b.WriteString("| Tier | Provider | Model | Task | Run | Result | HTTP | ms | 429 | Compile | Test | Review | Cost | Quota |\n")
	b.WriteString("|---|---|---|---|---:|---|---:|---:|---:|---|---|---|---|---|\n")
	taskN := 0
	for _, rec := range recs {
		if rec.RecordType != RecordTaskRun {
			continue
		}
		taskN++
		result := "FAIL"
		if rec.Skip {
			result = "SKIP"
		} else if rec.Pass {
			result = "PASS"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %s | %s | %d | %d | %s | %s | %s | %s | %s |\n",
			rec.Tier, rec.Provider, rec.ModelID, rec.Task, rec.Run, result,
			httpCell(rec.HTTPStatus), rec.LatencyMs, rec.Status429Count,
			boolCell(rec.CompileOK), boolCell(rec.TestOK), boolCell(rec.ReviewMatch),
			emptyDash(rec.ReportedCost), rec.QuotaScope)
	}
	if taskN == 0 {
		b.WriteString("| — | — | — | — | — | — | — | — | — | — | — | — | — | — |\n")
	}
	b.WriteString("\n## Scorecard\n\n")
	b.WriteString("Alphabetical within each tier. This table is not ordered by quality.\n\n")
	b.WriteString("| Tier | Provider | Model | Passes | Tasks | Mean ms | 429 |\n")
	b.WriteString("|---|---|---|---:|---:|---:|---:|\n")
	card := reg.Scorecard()
	if len(card) == 0 {
		b.WriteString("| — | — | — | — | — | — | — |\n")
	}
	for _, row := range card {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %d |\n",
			row.Tier, row.Provider, row.ModelID, row.Passes, row.Tasks, row.MeanLatencyMs, row.Status429)
	}
	b.WriteString("\n## Ranking\n\n")
	if !rank.Allowed {
		fmt.Fprintf(&b, "Ranking withheld: %s.\n", rank.Reason)
	} else {
		b.WriteString("Comparable order for this authenticated run only. Not a general provider ranking.\n\n")
		b.WriteString("| Order | Provider | Model | Passes | Tasks | Mean ms | 429 |\n")
		b.WriteString("|---:|---|---|---:|---:|---:|---:|\n")
		for i, row := range rank.Order {
			fmt.Fprintf(&b, "| %d | %s | %s | %d | %d | %d | %d |\n",
				i+1, row.Provider, row.ModelID, row.Passes, row.Tasks, row.MeanLatencyMs, row.Status429)
		}
		fmt.Fprintf(&b, "\nGate: %s.\n", rank.Reason)
	}
	b.WriteString("\n## Trial candidates\n\n")
	b.WriteString("Alphabetical by provider and model id. Eligibility for a later authenticated trial is not a usefulness ranking.\n\n")
	b.WriteString("| Tier | Provider | Model | Pricing | Tools | coding-best-free filter | Provenance |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	trials := 0
	for _, rec := range recs {
		if rec.RecordType != RecordTrial {
			continue
		}
		trials++
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
			rec.Tier, rec.Provider, rec.ModelID, rec.PricingMode, boolCell(rec.ToolsDeclared), yesNo(rec.CodingProfileEligible), rec.Provenance)
	}
	if trials == 0 {
		b.WriteString("| — | — | — | — | — | — | — |\n")
	}
	b.WriteString("\n## Catalog observations\n\n")
	b.WriteString("| Tier | Provider | HTTP | ms | Catalog rows | Note | Error |\n")
	b.WriteString("|---|---|---:|---:|---:|---|---|\n")
	cats := 0
	for _, rec := range recs {
		if rec.RecordType != RecordCatalog {
			continue
		}
		cats++
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %s | %s |\n",
			rec.Tier, rec.Provider, httpCell(rec.HTTPStatus), rec.LatencyMs, rec.CandidateCount, emptyDash(rec.Note), emptyDash(rec.Error))
	}
	if cats == 0 {
		b.WriteString("| — | — | — | — | — | — | — |\n")
	}
	b.WriteString("\n## Skips\n\n")
	skips := 0
	for _, rec := range recs {
		if rec.RecordType != RecordInferSkip && !(rec.RecordType == RecordTaskRun && rec.Skip) {
			continue
		}
		skips++
		fmt.Fprintf(&b, "- %s %s/%s %s: %s\n", rec.Tier, emptyDash(rec.Provider), emptyDash(rec.ModelID), emptyDash(rec.Task), rec.SkipReason)
	}
	if skips == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n## Qué no afirmar\n\n")
	b.WriteString("- No presentar el catálogo, el discovery ni los fixtures como un ranking de programación.\n")
	b.WriteString("- `coding-best-free` es un filtro de capacidades declaradas, no un resultado de este arnés.\n")
	b.WriteString("- Una medición autenticada, si existe, cubre solo las tareas bugfix, refactor y review de este arnés.\n")
	b.WriteString("- Un precio ausente no se rellena con cero. Una cuota desconocida queda como `unknown`.\n")
	b.WriteString("- Do not describe fixture self-checks or public catalog rows as measured coding quality.\n")
	if len(recs) > 0 && len(recs) <= 40 {
		b.WriteString("\n## Raw JSONL\n\n~~~\n")
		var raw strings.Builder
		if err := WriteJSONL(&raw, recs); err != nil {
			return err
		}
		b.WriteString(raw.String())
		b.WriteString("~~~\n")
	} else if len(recs) > 40 {
		b.WriteString("\nRaw JSONL is in `runs.jsonl` next to this report.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func httpCell(code int) string {
	if code == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d", code)
}

func boolCell(v *bool) string {
	if v == nil {
		return "n/a"
	}
	if *v {
		return "yes"
	}
	return "no"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return strings.ReplaceAll(s, "|", "/")
}
