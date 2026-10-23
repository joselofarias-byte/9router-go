package adaptive

import (
 "math"
 "fmt"
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

func TestDisabledShadowDoesNotRecord(t *testing.T) {
 t.Setenv("CAPIMUX_ADAPTIVE_SHADOW","0")
 r:=Route{Provider:"disabled-shadow-test",Model:"demo"}
 ObserveIfEnabled(Observation{Route:r,Latency:time.Second,Success:true})
 if got:=ShadowSnapshot([]Route{r})[0].Samples;got!=0 {t.Fatalf("disabled shadow recorded %d",got)}
}
func TestCollectorBounded(t *testing.T) {
 c:=NewCollector()
 for i:=0;i<maxRoutes+10;i++ {
  c.Observe(Observation{Route:Route{Provider:"p",Model:fmt.Sprintf("m%d",i)},Latency:time.Second,Success:true})
 }
 if len(c.routes)!=maxRoutes {t.Fatalf("route cap failed: %d",len(c.routes))}
}

func TestEnabledShadowRecordsOnlyExplicitFlag(t *testing.T) {
 r:=Route{Provider:"enabled-shadow-test",Model:"demo"}
 t.Setenv("CAPIMUX_ADAPTIVE_SHADOW","true")
 ObserveIfEnabled(Observation{Route:r,Latency:time.Second,Success:true})
 if got:=ShadowSnapshot([]Route{r})[0].Samples;got!=0 {t.Fatalf("non-1 shadow flag recorded %d",got)}
 t.Setenv("CAPIMUX_ADAPTIVE_SHADOW","1")
 ObserveIfEnabled(Observation{Route:r,Latency:time.Second,Success:true})
 snapshot:=ShadowSnapshot([]Route{r})[0]
 if snapshot.Samples!=1 || snapshot.SuccessRate!=1 {t.Fatalf("expected one successful shadow observation: %+v",snapshot)}
 t.Setenv("CAPIMUX_ADAPTIVE_SHADOW","0")
 ObserveIfEnabled(Observation{Route:r,Latency:time.Second,Success:false})
 if got:=ShadowSnapshot([]Route{r})[0].Samples;got!=1 {t.Fatalf("disabling shadow failed: got %d",got)}
}
