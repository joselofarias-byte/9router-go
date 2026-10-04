package freecoding

import (
	"encoding/json"
	"time"

	"9router/proxy/internal/controlplane/discovery"
)

const (
	noteEligible = "coding-best-free filter match (free pricing and tools declared). Catalog eligibility only, not a coding score."
	noteFreeOnly = "free catalog entry; tools are not declared, so coding-best-free does not select it. Not a quality score."
)

// CollectTrials keeps models the discovery adapters classified as free or
// free_tier. Paid and unknown rows are dropped. The slice is alphabetical
// by provider then model id, which is a stable display order, not a ranking.
func CollectTrials(cands []discovery.Candidate, tier, provenance string, now time.Time) []Record {
	utc := now.UTC().Format(time.RFC3339)
	var out []Record
	for _, c := range cands {
		if c.PricingMode != "free" && c.PricingMode != "free_tier" {
			continue
		}
		if c.ProviderID == "" || c.ModelID == "" {
			continue
		}
		tools := toolsDeclared(c.Capabilities)
		eligible := tools != nil && *tools
		note := noteFreeOnly
		if eligible {
			note = noteEligible
		}
		out = append(out, Record{
			RecordType:            RecordTrial,
			UTC:                   utc,
			Tier:                  tier,
			Provider:              c.ProviderID,
			ModelID:               c.ModelID,
			PricingMode:           c.PricingMode,
			ToolsDeclared:         tools,
			CodingProfileEligible: eligible,
			Provenance:            provenance,
			QuotaScope:            QuotaNotApplicable,
			Note:                  note,
		})
	}
	sortTrials(out)
	return out
}

func toolsDeclared(capabilities string) *bool {
	if capabilities == "" {
		return nil
	}
	var caps struct {
		Tools *bool `json:"tools"`
	}
	if err := json.Unmarshal([]byte(capabilities), &caps); err != nil {
		return nil
	}
	return caps.Tools
}

func sortTrials(recs []Record) {
	for i := 1; i < len(recs); i++ {
		j := i
		for j > 0 && trialLess(recs[j], recs[j-1]) {
			recs[j], recs[j-1] = recs[j-1], recs[j]
			j--
		}
	}
}

func trialLess(a, b Record) bool {
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	return a.ModelID < b.ModelID
}

// AllowList is the set of provider/model ids a discovery or fixture pass
// classified as free or free_tier. Inference refuses anything outside it.
type AllowList struct {
	models map[string]struct{}
}

func LoadAllowlist(recs []Record) AllowList {
	a := AllowList{models: map[string]struct{}{}}
	for _, rec := range recs {
		if rec.RecordType != RecordTrial {
			continue
		}
		if rec.PricingMode != "free" && rec.PricingMode != "free_tier" {
			continue
		}
		a.models[rec.Provider+"\x00"+rec.ModelID] = struct{}{}
	}
	return a
}

func (a AllowList) Has(provider, modelID string) bool {
	if a.models == nil {
		return false
	}
	_, ok := a.models[provider+"\x00"+modelID]
	return ok
}
