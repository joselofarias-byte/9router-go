package discovery

import (
 "context"
 "testing"
)

func TestLocalModelsAdapterUnknownPricingAndProviderIdentity(t *testing.T) {
 a, err := NewLocalModelsAdapter(`{"antigravity":["gemini-test","gemini-test"],"codex":["gpt-test"]}`)
 if err != nil { t.Fatal(err) }
 offers, err := a.Discover(context.Background())
 if err != nil { t.Fatal(err) }
 if len(offers) != 2 { t.Fatalf("got %d offers, want 2", len(offers)) }
 for _, offer := range offers {
  if offer.PricingMode != "unknown" { t.Fatalf("unexpected free classification: %#v", offer) }
  if offer.ProviderID != "antigravity" && offer.ProviderID != "codex" { t.Fatalf("provider mismatch: %#v", offer) }
  if offer.UpstreamModel != offer.ModelID { t.Fatalf("upstream mismatch: %#v", offer) }
 }
}

func TestLocalModelsAdapterRejectsUnverifiedProvidersAndBadIDs(t *testing.T) {
 for _, input := range []string{
  `{"google":["gemini-test"]}`,
  `{"codex":[""]}`,
  `{"antigravity":[" padded "]}`,
  `not-json`,
 } {
  if _, err := NewLocalModelsAdapter(input); err == nil { t.Fatalf("accepted invalid declaration %q", input) }
 }
}
