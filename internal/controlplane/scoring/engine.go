package scoring

import (
	"math"

	"9router/proxy/internal/controlplane/trust"
)

// Factors define the dimensions used to evaluate a node.
type Factors struct {
	TrustLevel   trust.TrustLevel
	LatencyMs    int
	TTFTMs       int
	SuccessRate  float64 // 0.0 to 1.0
	IsFreeTier   bool
	BenchmarkAvg float64 // 0.0 to 1.0 (Quality/Capability)
}

// Score represents the final calculated multi-dimensional score (0-100)
type Score struct {
	Total       float64
	Dimensions  map[string]float64
	IsRoutable  bool // If false, do not route to this node (e.g. quarantined)
}

// Calculate determines the routing score based on configurable weighted dimensions.
func Calculate(f Factors) Score {
	if f.TrustLevel == trust.TrustQuarantined || f.TrustLevel == trust.TrustDisabled {
		return Score{Total: 0, IsRoutable: false}
	}

	score := Score{
		Dimensions: make(map[string]float64),
		IsRoutable: true,
	}

	// 1. Trust Dimension (max 20 points)
	trustScore := 0.0
	switch f.TrustLevel {
	case trust.TrustTrusted: trustScore = 20.0
	case trust.TrustVerified: trustScore = 15.0
	case trust.TrustCandidate, trust.TrustUnknown: trustScore = 10.0
	case trust.TrustDegraded: trustScore = 5.0
	}
	score.Dimensions["trust"] = trustScore

	// 2. Success Rate (max 30 points)
	srScore := math.Max(0, math.Min(30, f.SuccessRate*30))
	score.Dimensions["success_rate"] = srScore

	// 3. Performance / TTFT (max 20 points)
	// Base ideal TTFT assumed around 500ms
	perfScore := 20.0
	if f.TTFTMs > 500 {
		penalty := float64(f.TTFTMs-500) / 100.0 // 1 point per 100ms over 500ms
		perfScore = math.Max(0, 20.0-penalty)
	} else if f.TTFTMs == 0 {
		perfScore = 10.0 // neutral if unknown
	}
	score.Dimensions["performance"] = perfScore

	// 4. Cost/Free Tier Bonus (max 10 points)
	costScore := 0.0
	if f.IsFreeTier {
		costScore = 10.0
	}
	score.Dimensions["cost"] = costScore

	// 5. Benchmark Quality (max 20 points)
	benchScore := math.Max(0, math.Min(20, f.BenchmarkAvg*20))
	score.Dimensions["quality"] = benchScore

	// Sum total
	total := trustScore + srScore + perfScore + costScore + benchScore
	score.Total = math.Max(0, math.Min(100, total))

	return score
}
