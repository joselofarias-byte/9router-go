package discovery

import (
 "context"
 "encoding/json"
 "fmt"
 "sort"
 "strings"
 "time"
)

// LocalModelsAdapter imports explicitly declared model IDs for supported local
// provider connections. Declarations do not establish free pricing or entitlement.
type LocalModelsAdapter struct {
 models map[string][]string
}

func NewLocalModelsAdapter(raw string) (*LocalModelsAdapter, error) {
 var models map[string][]string
 if err := json.Unmarshal([]byte(raw), &models); err != nil {
  return nil, fmt.Errorf("local model declarations: %w", err)
 }
 for provider, ids := range models {
  if provider != "antigravity" && provider != "codex" {
   return nil, fmt.Errorf("unsupported local provider %q", provider)
  }
  for _, id := range ids {
   if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
    return nil, fmt.Errorf("invalid model ID for %s", provider)
   }
  }
 }
 return &LocalModelsAdapter{models: models}, nil
}

func (a *LocalModelsAdapter) SourceID() string { return "local-declarations" }

func (a *LocalModelsAdapter) Discover(ctx context.Context) ([]Candidate, error) {
 now := time.Now().UTC()
 out := make([]Candidate, 0)
 providers := make([]string, 0, len(a.models))
 for provider := range a.models { providers = append(providers, provider) }
 sort.Strings(providers)
 for _, provider := range providers {
  seen := map[string]bool{}
  for _, id := range a.models[provider] {
   if err := ctx.Err(); err != nil { return nil, err }
   if seen[id] { continue }
   seen[id] = true
   out = append(out, Candidate{
    SourceID: a.SourceID(), Provenance: "explicit-local-declaration",
    Confidence: 0.5, RetrievalTime: now,
    ProviderID: provider, ModelID: id, UpstreamModel: id,
    PricingMode: "unknown",
   })
  }
 }
 return out, nil
}
