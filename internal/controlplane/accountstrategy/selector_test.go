package accountstrategy

import (
	"fmt"
	"testing"
	"time"
)

func f64(v float64) float64 { return v }

func TestSequentialDrainPrefersLowestKnownPositiveCapacity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 80, HasRemaining: true},
		{ID: "b", Status: StatusAvailable, Remaining: 20, HasRemaining: true},
		{ID: "unknown", Status: StatusUnknown},
	}, StrategySequentialDrain, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "b" {
		t.Fatalf("selected %q, want b: %+v", d.SelectedID, d)
	}
}

func TestResetDrainPrefersNearestFutureReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "weekly", Status: StatusAvailable, Remaining: 50, HasRemaining: true, ResetAt: now.Add(7 * 24 * time.Hour)},
		{ID: "soon", Status: StatusAvailable, Remaining: 50, HasRemaining: true, ResetAt: now.Add(2 * time.Hour)},
		{ID: "no-reset", Status: StatusAvailable, Remaining: 50, HasRemaining: true},
	}, StrategyResetDrain, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "soon" {
		t.Fatalf("selected %q, want soon: %+v", d.SelectedID, d)
	}
}

func TestElapsedResetDoesNotBecomeFreshCapacity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "stale", Status: StatusStale, Remaining: 100, HasRemaining: true, ResetAt: now.Add(-time.Minute)},
		{ID: "fresh", Status: StatusAvailable, Remaining: 10, HasRemaining: true, ResetAt: now.Add(time.Hour)},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "fresh" {
		t.Fatalf("selected %q, want fresh; stale reset must not imply replenishment: %+v", d.SelectedID, d)
	}
}

func TestCooldownAndExhaustedAreIneligible(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "cooling", Status: StatusAvailable, Remaining: 90, HasRemaining: true, CooldownUntil: now.Add(time.Minute)},
		{ID: "empty", Status: StatusExhausted},
		{ID: "ok", Status: StatusAvailable, Remaining: 5, HasRemaining: true},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "ok" {
		t.Fatalf("selected %q, want ok: %+v", d.SelectedID, d)
	}
	reasons := map[string]string{}
	for _, c := range d.Candidates {
		reasons[c.ID] = c.Reason
	}
	if reasons["cooling"] != "cooldown" || reasons["empty"] != "exhausted" {
		t.Fatalf("unexpected explain reasons: %+v", reasons)
	}
}

func TestAllCoolingReturnsNoSelection(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 90, HasRemaining: true, CooldownUntil: now.Add(time.Minute)},
		{ID: "b", Status: StatusUnknown, CooldownUntil: now.Add(time.Minute)},
	}, StrategySequentialDrain, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "" {
		t.Fatalf("selected %q, want none: %+v", d.SelectedID, d)
	}
}

func TestPreservedAccountIsUsedOnlyWhenNeeded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "preserve", Status: StatusAvailable, Remaining: 95, HasRemaining: true, Preserve: true},
		{ID: "normal", Status: StatusAvailable, Remaining: 10, HasRemaining: true},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "normal" {
		t.Fatalf("selected %q, want normal: %+v", d.SelectedID, d)
	}

	d, err = Select([]Candidate{
		{ID: "preserve", Status: StatusAvailable, Remaining: 95, HasRemaining: true, Preserve: true},
		{ID: "normal", Status: StatusExhausted},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "preserve" {
		t.Fatalf("selected %q, want preserve as final eligible account: %+v", d.SelectedID, d)
	}
}

func TestAutoSwitchDisabledHonorsPreferredAccountOnly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 10, HasRemaining: true},
		{ID: "b", Status: StatusAvailable, Remaining: 90, HasRemaining: true},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: false, PreferredID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "a" {
		t.Fatalf("selected %q, want preferred a: %+v", d.SelectedID, d)
	}

	d, err = Select([]Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 10, HasRemaining: true, CooldownUntil: now.Add(time.Minute)},
		{ID: "b", Status: StatusAvailable, Remaining: 90, HasRemaining: true},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: false, PreferredID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "" {
		t.Fatalf("selected %q, want none because automatic switching is disabled: %+v", d.SelectedID, d)
	}
}

func TestRelativeAvailabilityIsStableForSameHashKeyAndTopK(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cs := []Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 100, HasRemaining: true},
		{ID: "b", Status: StatusAvailable, Remaining: 80, HasRemaining: true},
		{ID: "c", Status: StatusAvailable, Remaining: 60, HasRemaining: true},
	}
	opts := Options{Now: now, AutoSwitch: true, HashKey: "request-42", TopK: 2, RelativePower: 2}
	a, err := Select(cs, StrategyRelativeAvailability, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Select(cs, StrategyRelativeAvailability, opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.SelectedID != b.SelectedID {
		t.Fatalf("same key selected %q then %q", a.SelectedID, b.SelectedID)
	}
	if a.SelectedID == "c" {
		t.Fatalf("top-k=2 selected excluded candidate c: %+v", a)
	}
	for _, e := range a.Candidates {
		if e.ID == "c" && e.Weight != 0 {
			t.Fatalf("excluded candidate c has non-zero weight: %+v", e)
		}
	}
}

func TestCapacityWeightedHashDistribution(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name       string
		aWeight    float64
		bWeight    float64
		higherID   string
		minWinners int
		maxWinners int
	}{
		{name: "80/20", aWeight: 80, bWeight: 20, higherID: "account-prefix-a", minWinners: 7500, maxWinners: 8500},
		{name: "20/80", aWeight: 20, bWeight: 80, higherID: "account-prefix-b", minWinners: 7500, maxWinners: 8500},
		{name: "equal", aWeight: 1, bWeight: 1, higherID: "account-prefix-a", minWinners: 4500, maxWinners: 5500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := []Candidate{
				{ID: "account-prefix-a", Status: StatusAvailable, Remaining: tt.aWeight, HasRemaining: true},
				{ID: "account-prefix-b", Status: StatusAvailable, Remaining: tt.bWeight, HasRemaining: true},
			}
			winners := 0
			for i := 0; i < 10_000; i++ {
				decision, err := Select(candidates, StrategyCapacityWeighted, Options{
					Now: now, AutoSwitch: true, HashKey: fmt.Sprintf("request-%d", i),
				})
				if err != nil {
					t.Fatal(err)
				}
				if decision.SelectedID == tt.higherID {
					winners++
				}
			}
			t.Logf("%s selected %d/10000 times", tt.higherID, winners)
			if winners < tt.minWinners || winners > tt.maxWinners {
				t.Fatalf("%s selected %d/10000 times, want range [%d,%d]", tt.higherID, winners, tt.minWinners, tt.maxWinners)
			}
		})
	}
}

func TestWeightedHashIsCandidateOrderIndependent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	forward := []Candidate{
		{ID: "shared-prefix-alpha", Status: StatusAvailable, Remaining: 80, HasRemaining: true},
		{ID: "shared-prefix-beta", Status: StatusAvailable, Remaining: 20, HasRemaining: true},
	}
	reverse := []Candidate{forward[1], forward[0]}
	for i := 0; i < 1_000; i++ {
		opts := Options{Now: now, AutoSwitch: true, HashKey: fmt.Sprintf("request-%d", i)}
		a, err := Select(forward, StrategyCapacityWeighted, opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Select(reverse, StrategyCapacityWeighted, opts)
		if err != nil {
			t.Fatal(err)
		}
		if a.SelectedID != b.SelectedID {
			t.Fatalf("key %q selected %q then %q after permutation", opts.HashKey, a.SelectedID, b.SelectedID)
		}
	}
}

func TestWeightedHashPairEncodingSeparatesDelimiters(t *testing.T) {
	if hashUnit("a|b", "c") == hashUnit("a", "b|c") {
		t.Fatal("length-prefixed hash pair encoding aliased delimiter-bearing inputs")
	}
}

func TestCapacityWeightedWithoutHashRemainsDeterministic(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	candidates := []Candidate{
		{ID: "lower", Status: StatusAvailable, Remaining: 20, HasRemaining: true},
		{ID: "higher", Status: StatusAvailable, Remaining: 80, HasRemaining: true},
	}
	decision, err := Select(candidates, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if decision.SelectedID != "higher" {
		t.Fatalf("selected %q without hash key, want higher", decision.SelectedID)
	}
}

func TestCapacityWeightedFallsBackToUnknownOnlyWhenNoFreshKnownCapacity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "unknown", Status: StatusUnknown, Priority: 20},
		{ID: "stale", Status: StatusStale, Priority: 10},
	}, StrategyCapacityWeighted, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "unknown" {
		t.Fatalf("selected %q, want highest-priority unknown fallback: %+v", d.SelectedID, d)
	}
}

func TestFillFirstUsesStablePriorityThenID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "z", Status: StatusUnknown, Priority: 5},
		{ID: "a", Status: StatusUnknown, Priority: 5},
		{ID: "low", Status: StatusAvailable, Remaining: 99, HasRemaining: true, Priority: 1},
	}, StrategyFillFirst, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "a" {
		t.Fatalf("selected %q, want a: %+v", d.SelectedID, d)
	}
}

func TestSingleAccountRequiresPreferredID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 50, HasRemaining: true},
	}, StrategySingleAccount, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "" {
		t.Fatalf("selected %q without preferred account", d.SelectedID)
	}

	d, err = Select([]Candidate{
		{ID: "a", Status: StatusAvailable, Remaining: 50, HasRemaining: true},
	}, StrategySingleAccount, Options{Now: now, AutoSwitch: true, PreferredID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "a" {
		t.Fatalf("selected %q, want a", d.SelectedID)
	}
}

func TestUnknownStrategyReturnsError(t *testing.T) {
	_, err := Select([]Candidate{{ID: "a", Status: StatusUnknown}}, Strategy("bogus"), Options{AutoSwitch: true})
	if err == nil {
		t.Fatal("expected error")
	}
}


func TestExpiryPressurePrefersLargestQuotaAtRiskOfExpiring(t *testing.T) {
	now := time.Date(2026, 10, 5, 3, 43, 0, 0, time.UTC)
	d, err := Select([]Candidate{
		{ID: "colacola", Status: StatusAvailable, Remaining: 42.48672, HasRemaining: true, ResetAt: now.Add(4*time.Hour + 7*time.Minute)},
		{ID: "jolufa", Status: StatusAvailable, Remaining: 96.53816, HasRemaining: true, ResetAt: now.Add(4*time.Hour + 12*time.Minute)},
		{ID: "ci", Status: StatusAvailable, Remaining: 84.96752, HasRemaining: true, ResetAt: now.Add(5 * time.Hour)},
		{ID: "joselo", Status: StatusAvailable, Remaining: 100, HasRemaining: true, ResetAt: now.Add(5*time.Hour + time.Minute)},
	}, StrategyExpiryPressure, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "jolufa" {
		t.Fatalf("selected %q, want jolufa: %+v", d.SelectedID, d)
	}

	weights := map[string]float64{}
	for _, c := range d.Candidates {
		weights[c.ID] = c.Weight
	}
	if !(weights["jolufa"] > weights["joselo"] &&
		weights["joselo"] > weights["ci"] &&
		weights["ci"] > weights["colacola"]) {
		t.Fatalf("unexpected pressure ordering: %+v", weights)
	}
}

func TestExpiryPressureIgnoresElapsedResetEvidence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "elapsed", Status: StatusAvailable, Remaining: 100, HasRemaining: true, ResetAt: now.Add(-time.Minute)},
		{ID: "future", Status: StatusAvailable, Remaining: 20, HasRemaining: true, ResetAt: now.Add(time.Hour)},
	}, StrategyExpiryPressure, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "future" {
		t.Fatalf("selected %q, want future; elapsed reset must not imply fresh quota: %+v", d.SelectedID, d)
	}
}

func TestExpiryPressureFallsBackWhenNoFutureResetKnown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d, err := Select([]Candidate{
		{ID: "unknown-reset-b", Status: StatusAvailable, Remaining: 80, HasRemaining: true, Priority: 1},
		{ID: "unknown-reset-a", Status: StatusAvailable, Remaining: 90, HasRemaining: true, Priority: 2},
	}, StrategyExpiryPressure, Options{Now: now, AutoSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedID != "unknown-reset-a" {
		t.Fatalf("selected %q, want stable priority fallback: %+v", d.SelectedID, d)
	}
}
