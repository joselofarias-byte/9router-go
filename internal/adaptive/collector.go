// Package adaptive provides opt-in, in-memory shadow scoring for provider/model routes.
// It does not perform HTTP calls, change fallback order, store credentials or alter billing.
package adaptive

import (
 "math"
 "os"
 "strings"
 "sync"
 "time"
)

type Route struct {
 Provider string
 Model string
}
type Observation struct {
 Route Route
 Latency time.Duration
 Success bool
 Tokens int64
 CostUSD float64
}
type Snapshot struct {
 Route Route `json:"route"`
 Samples uint64 `json:"samples"`
 SuccessRate float64 `json:"success_rate"`
 MeanLatencyMS float64 `json:"mean_latency_ms"`
 TokensPerSecond float64 `json:"tokens_per_second"`
 MeanCostUSD float64 `json:"mean_cost_usd"`
 Score float64 `json:"score"`
}
type counters struct {
 samples uint64
 successes uint64
 latencyMS float64
 tokens int64
 durationS float64
 costUSD float64
}
// Collector aggregates no prompts, responses, account identifiers or credentials.
// A process restart resets all counters; concurrency is guarded by a mutex.
const maxRoutes = 512

var shadowCollector = NewCollector()

// ObserveIfEnabled is disabled unless CAPIMUX_ADAPTIVE_SHADOW=1 is set.
// The environment flag is deliberately read at each call to simplify opt-out.
func ObserveIfEnabled(o Observation) {
 if strings.TrimSpace(os.Getenv("CAPIMUX_ADAPTIVE_SHADOW"))!="1" {return}
 shadowCollector.Observe(o)
}

// ShadowSnapshot gives diagnostics to in-package callers without HTTP exposure.
func ShadowSnapshot(routes []Route) []Snapshot {return shadowCollector.Snapshot(routes)}

type Collector struct {
 mu sync.RWMutex
 routes map[Route]counters
}
func NewCollector() *Collector { return &Collector{routes:make(map[Route]counters)} }
func (c *Collector) Observe(o Observation) {
 if c==nil || o.Route.Provider=="" || o.Route.Model=="" || o.Latency<=0 || o.Tokens<0 ||
   math.IsNaN(o.CostUSD) || math.IsInf(o.CostUSD, 0) || o.CostUSD<0 { return }
 c.mu.Lock()
 defer c.mu.Unlock()
 if c.routes==nil { c.routes=make(map[Route]counters) }
 v,exists:=c.routes[o.Route]
 if !exists && len(c.routes)>=maxRoutes {return}
 v.samples++
 if o.Success { v.successes++; v.tokens+=o.Tokens; v.durationS+=o.Latency.Seconds(); v.costUSD+=o.CostUSD }
 v.latencyMS+=float64(o.Latency)/float64(time.Millisecond)
 c.routes[o.Route]=v
}
func (c *Collector) Snapshot(routes []Route) []Snapshot {
 if c==nil {return nil}
 c.mu.RLock()
 defer c.mu.RUnlock()
 out:=make([]Snapshot,0,len(routes))
 for _,route:=range routes {
  v:=c.routes[route]
  s:=Snapshot{Route:route,Samples:v.samples}
  if v.samples>0 {
   s.SuccessRate=float64(v.successes)/float64(v.samples)
   s.MeanLatencyMS=v.latencyMS/float64(v.samples)
  }
  if v.durationS>0 {s.TokensPerSecond=float64(v.tokens)/v.durationS}
  if v.successes>0 {s.MeanCostUSD=v.costUSD/float64(v.successes)}
  // Shadow-only heuristic, bounded in [0,100]. Unknown routes receive neutral 50.
  s.Score=50
  if v.samples>0 {
   reliability:=s.SuccessRate*70
   latency:=20/(1+s.MeanLatencyMS/2000)
   throughput:=10*math.Min(1,s.TokensPerSecond/50)
   s.Score=reliability+latency+throughput
  }
  out=append(out,s)
 }
 return out
}
