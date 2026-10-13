package accountstrategy

import (
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
