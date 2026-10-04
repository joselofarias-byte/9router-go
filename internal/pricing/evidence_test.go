package pricing

import (
	"math"
	"testing"
)

func TestEstimateCostEvidenceDistinguishesFreeFromUnknown(t *testing.T) {
	tokens := TokenCounts{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	free := EstimateCostEvidence("cline", "cline-free/deepseek-v4.1-flash", tokens)
	if !free.Known || free.CostUSD != 0 {
		t.Fatalf("free evidence = %+v, want known zero", free)
	}

	unknown := EstimateCostEvidence("x", "totally-unknown-model", tokens)
	if unknown.Known || unknown.CostUSD != 0 {
		t.Fatalf("unknown evidence = %+v, want unknown zero placeholder", unknown)
	}
}

func TestCompareCostToBaselineOnlyClaimsDeltaWhenBothPricesKnown(t *testing.T) {
	tokens := TokenCounts{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	cmp := CompareCostToBaseline(
		"cline", "cline-free/deepseek-v4.1-flash",
		"openai", "gpt-4o",
		tokens,
	)
	if !cmp.Comparable {
		t.Fatalf("comparison should be auditable: %+v", cmp)
	}
	// gpt-4o is $2.50/M input + $10/M output for this token shape.
	if math.Abs(cmp.DeltaUSD-12.5) > 1e-9 {
		t.Fatalf("delta = %v, want 12.5: %+v", cmp.DeltaUSD, cmp)
	}

	cmp = CompareCostToBaseline(
		"cline", "cline-free/deepseek-v4.1-flash",
		"x", "totally-unknown-model",
		tokens,
	)
	if cmp.Comparable || cmp.DeltaUSD != 0 {
		t.Fatalf("unknown baseline must not produce savings: %+v", cmp)
	}
}

func TestCompareCostToBaselineCanReportNegativeDelta(t *testing.T) {
	tokens := TokenCounts{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	cmp := CompareCostToBaseline(
		"openai", "gpt-5.6-sol",
		"openai", "gpt-4o",
		tokens,
	)
	if !cmp.Comparable {
		t.Fatalf("comparison should be known: %+v", cmp)
	}
	if cmp.DeltaUSD >= 0 {
		t.Fatalf("expected negative delta when actual route costs more: %+v", cmp)
	}
}
