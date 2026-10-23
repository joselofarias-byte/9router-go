package adaptive

import (
 "encoding/json"
 "sort"
)

// Export is a privacy-preserving, in-process snapshot of the shadow collector.
// No accounts, prompts, credentials, or HTTP endpoints are exposed.
type Export struct {
 SchemaVersion int        `json:"schema_version"`
 Routes        []Snapshot `json:"routes"`
}

// ExportJSON serializes a stable diagnostic snapshot of every observed route.
// It does not persist data or activate adaptive routing.
func (c *Collector) ExportJSON() ([]byte, error) {
 routes := make([]Route, 0)
 if c != nil {
  c.mu.RLock()
  routes = make([]Route, 0, len(c.routes))
  for r := range c.routes { routes = append(routes, r) }
  c.mu.RUnlock()
 }
 sort.Slice(routes, func(i,j int) bool {
  if routes[i].Provider != routes[j].Provider { return routes[i].Provider < routes[j].Provider }
  return routes[i].Model < routes[j].Model
 })
 snapshots := make([]Snapshot, 0, len(routes))
 if c != nil { snapshots = c.Snapshot(routes) }
 return json.Marshal(Export{SchemaVersion:1, Routes:snapshots})
}

// ShadowExportJSON only exports existing in-memory metrics.
// It never enables collection; callers must enforce their own authorization.
func ShadowExportJSON() ([]byte,error) {
 return shadowCollector.ExportJSON()
}
