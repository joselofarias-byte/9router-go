package pricing

// CostEvidence distinguishes a known zero-dollar route from an unknown price.
//
// This is intentionally additive. EstimateCost keeps its upstream-compatible
// behavior of returning 0 for unknown prices, while product/analytics code can
// use EstimateCostEvidence when it needs auditable money semantics.
type CostEvidence struct {
	Provider string       `json:"provider"`
	Model    string       `json:"model"`
	CostUSD  float64      `json:"costUsd"`
	Known    bool         `json:"known"`
	Pricing  ModelPricing `json:"pricing"`
}

// EstimateCostEvidence resolves pricing and reports whether the cost is known.
// Free models are Known=true with CostUSD=0. Unpriced models are Known=false;
// callers must not treat that case as free.
func EstimateCostEvidence(provider, model string, tokens TokenCounts) CostEvidence {
	p, ok := GetPricingForModel(provider, model)
	if !ok {
		return CostEvidence{Provider: provider, Model: model, Known: false}
	}
	return CostEvidence{
		Provider: provider,
		Model:    model,
		CostUSD:  CalculateCost(tokens, p),
		Known:    true,
		Pricing:  p,
	}
}

// CostComparison compares the actual routed request with an explicit baseline.
//
// DeltaUSD = baseline - actual. Positive means the actual route was cheaper;
// negative means it was more expensive. Comparable is false unless both sides
// have known pricing, preventing fabricated "savings" from unknown prices.
type CostComparison struct {
	Actual     CostEvidence `json:"actual"`
	Baseline   CostEvidence `json:"baseline"`
	Comparable bool         `json:"comparable"`
	DeltaUSD   float64      `json:"deltaUsd,omitempty"`
}

// CompareCostToBaseline produces auditable cost evidence for one token shape.
// The caller must choose the baseline explicitly; this package never invents
// one from a product tier, subscription, or vendor default.
func CompareCostToBaseline(
	actualProvider, actualModel string,
	baselineProvider, baselineModel string,
	tokens TokenCounts,
) CostComparison {
	actual := EstimateCostEvidence(actualProvider, actualModel, tokens)
	baseline := EstimateCostEvidence(baselineProvider, baselineModel, tokens)
	out := CostComparison{Actual: actual, Baseline: baseline}
	if actual.Known && baseline.Known {
		out.Comparable = true
		out.DeltaUSD = baseline.CostUSD - actual.CostUSD
	}
	return out
}
