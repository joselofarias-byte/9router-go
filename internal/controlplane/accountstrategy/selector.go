// Package accountstrategy contains provider-agnostic account selection policies.
// It deliberately knows nothing about Codex, ChatGPT, Gemini, Antigravity or any
// other product. Callers translate their capacity/availability observations into
// Candidate values, then consume the explainable Decision.
package accountstrategy

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

type Strategy string

const (
	StrategyCapacityWeighted     Strategy = "capacity_weighted"
	StrategyRelativeAvailability Strategy = "relative_availability"
	StrategySequentialDrain      Strategy = "sequential_drain"
	StrategyResetDrain           Strategy = "reset_drain"
	StrategyFillFirst            Strategy = "fill_first"
	StrategySingleAccount        Strategy = "single_account"
)

type Status string

const (
	StatusUnknown   Status = "unknown"
	StatusStale     Status = "stale"
	StatusAvailable Status = "available"
	StatusExhausted Status = "exhausted"
)

// Candidate is the generic routing view of one configured account.
//
// Remaining is meaningful only when StatusAvailable and HasRemaining are both
// true. Unknown/stale observations stay eligible fallbacks but are never
// interpreted as unlimited capacity.
//
// Preserve is an operator hint: automatic strategies avoid preserved accounts
// while another eligible non-preserved account exists.
type Candidate struct {
	ID            string
	Status        Status
	Remaining     float64
	HasRemaining  bool
	ResetAt       time.Time
	CooldownUntil time.Time
	Disabled      bool
	Preserve      bool
	Priority      int
}

type Options struct {
	Now time.Time

	// HashKey should vary per request when weighted distribution is desired.
	// The same key produces a stable choice, which is useful for sticky flows.
	HashKey string

	// PreferredID is required by single_account and is the only account that
	// may be selected when AutoSwitch is false.
	PreferredID string

	// AutoSwitch controls whether automatic account switching is allowed for
	// this provider/account type. It is intentionally explicit because provider
	// policy may prohibit automatic rotation.
	AutoSwitch bool

	// RelativePower and TopK tune relative_availability.
	// Zero values mean power=1 and topK=3.
	RelativePower float64
	TopK          int
}

type Evaluation struct {
	ID        string  `json:"id"`
	Eligible  bool    `json:"eligible"`
	Reason    string  `json:"reason,omitempty"`
	Weight    float64 `json:"weight,omitempty"`
	Preserve  bool    `json:"preserve,omitempty"`
	Priority  int     `json:"priority,omitempty"`
	Remaining float64 `json:"remaining,omitempty"`
	Known     bool    `json:"known"`
}

type Decision struct {
	Strategy   Strategy     `json:"strategy"`
	SelectedID string       `json:"selectedId,omitempty"`
	Candidates []Evaluation `json:"candidates"`
}

var ErrUnknownStrategy = errors.New("unknown account selection strategy")

// Select evaluates eligibility first, then applies a provider-agnostic
// selection policy. It never invents quota from reset timestamps or cooldowns.
func Select(candidates []Candidate, strategy Strategy, opts Options) (Decision, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.RelativePower <= 0 {
		opts.RelativePower = 1
	}
	if opts.TopK <= 0 {
		opts.TopK = 3
	}

	decision := Decision{Strategy: strategy, Candidates: make([]Evaluation, 0, len(candidates))}
	eligible := make([]Candidate, 0, len(candidates))

	for _, c := range candidates {
		e := Evaluation{
			ID:        c.ID,
			Preserve:  c.Preserve,
			Priority:  c.Priority,
			Remaining: c.Remaining,
			Known:     c.Status == StatusAvailable && c.HasRemaining,
		}
		switch {
		case strings.TrimSpace(c.ID) == "":
			e.Reason = "missing_account_id"
		case c.Disabled:
			e.Reason = "disabled"
		case c.Status == StatusExhausted:
			e.Reason = "exhausted"
		case !c.CooldownUntil.IsZero() && opts.Now.Before(c.CooldownUntil):
			e.Reason = "cooldown"
		case c.Status == StatusAvailable && c.HasRemaining && c.Remaining <= 0:
			e.Reason = "exhausted"
		default:
			e.Eligible = true
			eligible = append(eligible, c)
		}
		decision.Candidates = append(decision.Candidates, e)
	}

	if !opts.AutoSwitch {
		if opts.PreferredID == "" {
			return decision, nil
		}
		if containsID(eligible, opts.PreferredID) {
			decision.SelectedID = opts.PreferredID
		}
		return decision, nil
	}

	if strategy == StrategySingleAccount {
		if opts.PreferredID != "" && containsID(eligible, opts.PreferredID) {
			decision.SelectedID = opts.PreferredID
		}
		return decision, nil
	}

	eligible = preferNonPreserved(eligible)
	if len(eligible) == 0 {
		return decision, nil
	}

	var selected string
	var weights map[string]float64
	switch strategy {
	case StrategyCapacityWeighted:
		selected, weights = selectCapacityWeighted(eligible, opts.HashKey)
	case StrategyRelativeAvailability:
		selected, weights = selectRelativeAvailability(eligible, opts)
	case StrategySequentialDrain:
		selected = selectSequentialDrain(eligible)
	case StrategyResetDrain:
		selected = selectResetDrain(eligible, opts.Now)
	case StrategyFillFirst:
		selected = selectFillFirst(eligible)
	default:
		return Decision{}, ErrUnknownStrategy
	}

	decision.SelectedID = selected
	if len(weights) > 0 {
		for i := range decision.Candidates {
			decision.Candidates[i].Weight = weights[decision.Candidates[i].ID]
		}
	}
	return decision, nil
}

func containsID(cs []Candidate, id string) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}

func preferNonPreserved(cs []Candidate) []Candidate {
	hasNormal := false
	for _, c := range cs {
		if !c.Preserve {
			hasNormal = true
			break
		}
	}
	if !hasNormal {
		return cs
	}
	out := make([]Candidate, 0, len(cs))
	for _, c := range cs {
		if !c.Preserve {
			out = append(out, c)
		}
	}
	return out
}

func knownAvailable(cs []Candidate) []Candidate {
	out := make([]Candidate, 0, len(cs))
	for _, c := range cs {
		if c.Status == StatusAvailable && c.HasRemaining && c.Remaining > 0 {
			out = append(out, c)
		}
	}
	return out
}

func stableFallback(cs []Candidate) string {
	cp := append([]Candidate(nil), cs...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Priority != cp[j].Priority {
			return cp[i].Priority > cp[j].Priority
		}
		return cp[i].ID < cp[j].ID
	})
	if len(cp) == 0 {
		return ""
	}
	return cp[0].ID
}

func selectCapacityWeighted(cs []Candidate, key string) (string, map[string]float64) {
	known := knownAvailable(cs)
	if len(known) == 0 {
		return stableFallback(cs), nil
	}
	weights := make(map[string]float64, len(known))
	for _, c := range known {
		weights[c.ID] = math.Max(c.Remaining, 0.000001)
	}
	if key == "" {
		sort.SliceStable(known, func(i, j int) bool {
			if known[i].Remaining != known[j].Remaining {
				return known[i].Remaining > known[j].Remaining
			}
			if known[i].Priority != known[j].Priority {
				return known[i].Priority > known[j].Priority
			}
			return known[i].ID < known[j].ID
		})
		return known[0].ID, weights
	}
	return weightedRendezvous(known, weights, key), weights
}

func selectRelativeAvailability(cs []Candidate, opts Options) (string, map[string]float64) {
	known := knownAvailable(cs)
	if len(known) == 0 {
		return stableFallback(cs), nil
	}
	sort.SliceStable(known, func(i, j int) bool {
		if known[i].Remaining != known[j].Remaining {
			return known[i].Remaining > known[j].Remaining
		}
		if known[i].Priority != known[j].Priority {
			return known[i].Priority > known[j].Priority
		}
		return known[i].ID < known[j].ID
	})
	if opts.TopK < len(known) {
		known = known[:opts.TopK]
	}
	maxRemaining := known[0].Remaining
	weights := make(map[string]float64, len(known))
	for _, c := range known {
		relative := c.Remaining / maxRemaining
		weights[c.ID] = math.Pow(math.Max(relative, 0.000001), opts.RelativePower)
	}
	if opts.HashKey == "" {
		return known[0].ID, weights
	}
	return weightedRendezvous(known, weights, opts.HashKey), weights
}

func weightedRendezvous(cs []Candidate, weights map[string]float64, key string) string {
	bestID := ""
	bestScore := -1.0
	for _, c := range cs {
		w := weights[c.ID]
		if w <= 0 {
			continue
		}
		u := hashUnit(key, c.ID)
		// Weighted rendezvous via an exponential race. Higher weight makes a
		// candidate more likely to win while the same key remains stable.
		score := w / -math.Log(u)
		if score > bestScore || (score == bestScore && c.ID < bestID) {
			bestScore = score
			bestID = c.ID
		}
	}
	return bestID
}

func hashUnit(key, accountID string) float64 {
	// Domain separation and length prefixes make the pair encoding unambiguous:
	// ("a|b", "c") and ("a", "b|c") cannot share the same encoded input.
	const domain = "9router-go/accountstrategy/weighted-rendezvous/v1\x00"
	payload := make([]byte, 0, len(domain)+16+len(key)+len(accountID))
	payload = append(payload, domain...)
	var fieldLength [8]byte
	binary.BigEndian.PutUint64(fieldLength[:], uint64(len(key)))
	payload = append(payload, fieldLength[:]...)
	payload = append(payload, key...)
	binary.BigEndian.PutUint64(fieldLength[:], uint64(len(accountID)))
	payload = append(payload, fieldLength[:]...)
	payload = append(payload, accountID...)
	digest := sha256.Sum256(payload)
	// Keep u strictly inside (0,1) so -log(u) is finite and positive.
	const denom = float64(uint64(1) << 53)
	return (float64(binary.BigEndian.Uint64(digest[:8])>>11) + 0.5) / denom
}

func selectSequentialDrain(cs []Candidate) string {
	known := knownAvailable(cs)
	if len(known) == 0 {
		return stableFallback(cs)
	}
	sort.SliceStable(known, func(i, j int) bool {
		if known[i].Remaining != known[j].Remaining {
			return known[i].Remaining < known[j].Remaining
		}
		if known[i].Priority != known[j].Priority {
			return known[i].Priority > known[j].Priority
		}
		return known[i].ID < known[j].ID
	})
	return known[0].ID
}

func selectResetDrain(cs []Candidate, now time.Time) string {
	known := knownAvailable(cs)
	if len(known) == 0 {
		return stableFallback(cs)
	}
	sort.SliceStable(known, func(i, j int) bool {
		ai := resetRank(known[i], now)
		aj := resetRank(known[j], now)
		if ai != aj {
			return ai < aj
		}
		if !known[i].ResetAt.Equal(known[j].ResetAt) {
			if known[i].ResetAt.IsZero() {
				return false
			}
			if known[j].ResetAt.IsZero() {
				return true
			}
			return known[i].ResetAt.Before(known[j].ResetAt)
		}
		if known[i].Remaining != known[j].Remaining {
			return known[i].Remaining < known[j].Remaining
		}
		return known[i].ID < known[j].ID
	})
	return known[0].ID
}

func resetRank(c Candidate, now time.Time) int {
	if c.ResetAt.IsZero() {
		return 1
	}
	if !c.ResetAt.After(now) {
		// An elapsed reset never proves replenishment. The caller should normally
		// have marked this observation stale; rank it behind a future reset.
		return 2
	}
	return 0
}

func selectFillFirst(cs []Candidate) string {
	cp := append([]Candidate(nil), cs...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Priority != cp[j].Priority {
			return cp[i].Priority > cp[j].Priority
		}
		return cp[i].ID < cp[j].ID
	})
	return cp[0].ID
}
