package freecoding

import "strings"

// Registry is the parsed measurement log used for scorecards and the
// ranking gate.
type Registry struct {
	Records []Record
}

func NewRegistry(recs []Record) *Registry {
	return &Registry{Records: append([]Record(nil), recs...)}
}

// ModelScore aggregates task runs for one provider/model inside one tier.
// Order in a scorecard is alphabetical. A quality order exists only on
// Ranking when Allowed is true.
type ModelScore struct {
	Tier          string
	Provider      string
	ModelID       string
	Passes        int
	Tasks         int
	MeanLatencyMs int64
	Status429     int
}

// Ranking is a comparison of authenticated task runs. Allowed is false
// unless every compared model completed bugfix, refactor, and review in
// the authenticated tier with no skips, and at least two models are present.
type Ranking struct {
	Allowed bool
	Reason  string
	Order   []ModelScore
}

type taskAgg struct {
	passes  int
	tasks   int
	latency int64
	n       int
	s429    int
	seen    map[string]bool
}

func (reg *Registry) taskRuns() []Record {
	var out []Record
	for _, rec := range reg.Records {
		if rec.RecordType == RecordTaskRun {
			out = append(out, rec)
		}
	}
	return out
}

// Scorecard lists task-run aggregates alphabetically within each tier.
// It is not a quality ranking.
func (reg *Registry) Scorecard() []ModelScore {
	groups := map[string]*taskAgg{}
	var keys []string
	for _, rec := range reg.taskRuns() {
		if rec.Skip {
			continue
		}
		key := rec.Tier + "\x00" + rec.Provider + "\x00" + rec.ModelID
		a := groups[key]
		if a == nil {
			a = &taskAgg{seen: map[string]bool{}}
			groups[key] = a
			keys = append(keys, key)
		}
		a.tasks++
		a.n++
		a.latency += rec.LatencyMs
		a.s429 += rec.Status429Count
		if rec.Pass {
			a.passes++
		}
		a.seen[rec.Task] = true
	}
	order := make([]ModelScore, 0, len(keys))
	for _, key := range keys {
		a := groups[key]
		parts := strings.SplitN(key, "\x00", 3)
		mean := int64(0)
		if a.n > 0 {
			mean = a.latency / int64(a.n)
		}
		order = append(order, ModelScore{
			Tier: parts[0], Provider: parts[1], ModelID: parts[2],
			Passes: a.passes, Tasks: a.tasks, MeanLatencyMs: mean, Status429: a.s429,
		})
	}
	sortScoresAlpha(order)
	return order
}

// Ranking decides whether a quality order may be published.
func (reg *Registry) Ranking() Ranking {
	runs := reg.taskRuns()
	if len(runs) == 0 {
		return Ranking{Reason: "no task runs"}
	}
	for _, rec := range runs {
		if rec.Tier != TierInference {
			return Ranking{Reason: "ranking requires authenticated_inference task runs only; catalog and fixture results are not a coding benchmark"}
		}
		if rec.Skip {
			return Ranking{Reason: "ranking requires completed runs; a skip is present"}
		}
	}
	groups := map[string]*taskAgg{}
	var keys []string
	for _, rec := range runs {
		key := rec.Provider + "\x00" + rec.ModelID
		a := groups[key]
		if a == nil {
			a = &taskAgg{seen: map[string]bool{}}
			groups[key] = a
			keys = append(keys, key)
		}
		a.tasks++
		a.n++
		a.latency += rec.LatencyMs
		a.s429 += rec.Status429Count
		if rec.Pass {
			a.passes++
		}
		a.seen[rec.Task] = true
	}
	for _, key := range keys {
		a := groups[key]
		for _, task := range []string{TaskBugfix, TaskRefactor, TaskReview} {
			if !a.seen[task] {
				return Ranking{Reason: "ranking requires every model to have bugfix, refactor, and review"}
			}
		}
	}
	if len(keys) < 2 {
		return Ranking{Reason: "a single model scorecard is not a comparison ranking"}
	}
	order := make([]ModelScore, 0, len(keys))
	for _, key := range keys {
		a := groups[key]
		parts := strings.SplitN(key, "\x00", 2)
		order = append(order, ModelScore{
			Tier: TierInference, Provider: parts[0], ModelID: parts[1],
			Passes: a.passes, Tasks: a.tasks, MeanLatencyMs: a.latency / int64(a.n), Status429: a.s429,
		})
	}
	sortScoresQuality(order)
	return Ranking{
		Allowed: true,
		Reason:  "comparable authenticated runs for bugfix, refactor, and review",
		Order:   order,
	}
}

func sortScoresAlpha(order []ModelScore) {
	for i := 1; i < len(order); i++ {
		j := i
		for j > 0 && scoreLessAlpha(order[j], order[j-1]) {
			order[j], order[j-1] = order[j-1], order[j]
			j--
		}
	}
}

func scoreLessAlpha(a, b ModelScore) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	return a.ModelID < b.ModelID
}

func sortScoresQuality(order []ModelScore) {
	for i := 1; i < len(order); i++ {
		j := i
		for j > 0 && scoreBetter(order[j], order[j-1]) {
			order[j], order[j-1] = order[j-1], order[j]
			j--
		}
	}
}

func scoreBetter(a, b ModelScore) bool {
	if a.Passes != b.Passes {
		return a.Passes > b.Passes
	}
	if a.MeanLatencyMs != b.MeanLatencyMs {
		return a.MeanLatencyMs < b.MeanLatencyMs
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	return a.ModelID < b.ModelID
}
