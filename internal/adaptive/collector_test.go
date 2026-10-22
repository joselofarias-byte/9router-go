package adaptive

import (
 "math"
 "sync"
 "testing"
 "time"
)
func TestObserveAndSnapshot(t *testing.T) {
 c:=NewCollector()
 r:=Route{Provider:"free",Model:"demo"}
 c.Observe(Observation{Route:r,Latency:time.Second,Success:true,Tokens:50})
 c.Observe(Observation{Route:r,Latency:3*time.Second,Success:false})
 ss:=c.Snapshot([]Route{r})
 if len(ss)!=1 || ss[0].Samples!=2 || ss[0].SuccessRate!=.5 || ss[0].MeanLatencyMS!=2000 {
  t.Fatalf("unexpected snapshot: %+v",ss)
 }
 if math.IsNaN(ss[0].Score) || ss[0].Score<0 || ss[0].Score>100 {t.Fatalf("bad score: %+v",ss[0])}
}
func TestInvalidInputDoesNotRecord(t *testing.T) {
 c:=NewCollector();r:=Route{Provider:"p",Model:"m"}
 c.Observe(Observation{Route:r,Latency:-time.Second,Success:true})
 c.Observe(Observation{Route:r,Latency:time.Second,CostUSD:math.NaN()})
 if n:=c.Snapshot([]Route{r})[0].Samples;n!=0 {t.Fatalf("expected zero, got %d",n)}
}
func TestConcurrentObserve(t *testing.T) {
 c:=NewCollector();r:=Route{Provider:"p",Model:"m"}
 var wg sync.WaitGroup
 for i:=0;i<100;i++ {wg.Add(1);go func(){defer wg.Done();c.Observe(Observation{Route:r,Latency:time.Millisecond,Success:true})}()}
 wg.Wait()
 if n:=c.Snapshot([]Route{r})[0].Samples;n!=100 {t.Fatalf("expected 100, got %d",n)}
}
func TestUnknownRouteIsNeutral(t *testing.T) {
 s:=NewCollector().Snapshot([]Route{{Provider:"new",Model:"unknown"}})
 if s[0].Score!=50 {t.Fatalf("unknown route score: %v",s[0].Score)}
}
