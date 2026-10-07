package chat

import (
 "fmt"
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "testing"

 "9router/proxy/internal/controlplane/registry"
 "9router/proxy/internal/controlplane/routing"
 "9router/proxy/internal/controlplane/trust"
 "9router/proxy/internal/db"
 "9router/proxy/internal/providers"
)

func TestFusionPanel_ConfiguredDestinationAndRetryableStatuses(t *testing.T) {
 for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
  t.Run(fmt.Sprint(status), func(t *testing.T) {
   var configuredHits, defaultHits atomic.Int32
   configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    configuredHits.Add(1)
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    w.Write([]byte(`{"error":{"message":"configured route denied"}}`))
   }))
   defer configured.Close()
   defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    defaultHits.Add(1)
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"choices":[{"message":{"content":"wrong destination"}}]}`))
   }))
   defer defaultUpstream.Close()
   database, cleanup := setupChatTestDB(t)
   defer cleanup()
   if _, err := database.Exec(`DELETE FROM providerConnections WHERE provider = 'opencode'`); err != nil { t.Fatal(err) }
   model := fmt.Sprintf("audit-fusion-destination-%d", status)
   connID := fmt.Sprintf("conn-fusion-destination-%d", status)
   seedConnDB(t, database, "opencode", connID, "", configured.URL)
   orig := providers.KnownProviders["opencode"]
   cfg := orig
   cfg.BaseURL = defaultUpstream.URL
   providers.KnownProviders["opencode"] = cfg
   defer func() { providers.KnownProviders["opencode"] = orig }()
   if err := registry.InitRegistry(nil); err != nil { t.Fatal(err) }
   oldTM, oldEngine := globalTrustManager, globalRoutingEngine
   tm := trust.NewManager()
   globalTrustManager = tm
   globalRoutingEngine = &routing.Engine{TrustManager: tm}
   defer func() { globalTrustManager = oldTM; globalRoutingEngine = oldEngine }()
   repo := db.NewRepo(database)
   h := NewChatHandler(repo)
   result := h.makePanelCall([]byte(`{"model":"ignored","messages":[{"role":"user","content":"hi"}],"stream":false}`), "opencode/"+model)()
   if result == nil || result.err == nil { t.Fatalf("expected configured upstream %d error, got %#v", status, result) }
   if got := configuredHits.Load(); got != 1 { t.Fatalf("configured destination hits = %d, want 1", got) }
   if got := defaultHits.Load(); got != 0 { t.Fatalf("global default destination was used: hits = %d", got) }
   locked, err := repo.IsConnectionModelLocked(connID, model)
   if err != nil { t.Fatal(err) }
   if !locked { t.Fatalf("real account/model not locked after %d", status) }
   if status == http.StatusUnauthorized || status == http.StatusForbidden {
    if level := tm.GetTrustLevel("opencode", model, connID); level != trust.TrustQuarantined { t.Fatalf("auth failure trust = %s, want quarantined", level) }
   }
  })
 }
}
