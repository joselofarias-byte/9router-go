package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/controlplane/trust"
	"9router/proxy/internal/db"
)

func TestHandleAdminRegistry_NoAuthData(t *testing.T) {
	// Setup mock state
	registry.InitRegistry(nil)
	state := registry.GetActiveState()
	state.Accounts["test-acc"] = &registry.Account{
		ID:       "test-acc",
		IsActive: true,
	}

	h := &ChatHandler{} // lightweight stub
	req, _ := http.NewRequest("GET", "/api/admin/registry", nil)
	rr := httptest.NewRecorder()

	h.HandleAdminRegistry(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rr.Code)
	}

	var res registry.RegistryState
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	acc := res.Accounts["test-acc"]
	if acc == nil {
		t.Fatal("Expected account in response")
	}
}

func TestHandleAdminExplainRoute(t *testing.T) {
	h := &ChatHandler{}
	req, _ := http.NewRequest("GET", "/api/admin/explain-route?model=gpt-4&policy=free-first", nil)
	rr := httptest.NewRecorder()

	h.HandleAdminExplainRoute(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rr.Code)
	}

	// Validate JSON response shape
	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if res["requestedModel"] != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got %v", res["requestedModel"])
	}
	if res["policy"] != "free-first" {
		t.Errorf("Expected policy 'free-first', got %v", res["policy"])
	}
}

// TestHandleAdminExplainRoute_UsesSharedTrustManager guards against a
// regression where the endpoint instantiated a throwaway trust.Manager
// instead of the shared one live traffic updates, which made every node
// always look untested regardless of its real observed trust state.
func TestHandleAdminExplainRoute_UsesSharedTrustManager(t *testing.T) {
	resetFabricRoutingStateForTests()
	defer resetFabricRoutingStateForTests()

	registry.InitRegistry(nil)
	state := registry.GetActiveState()
	state.Providers["trustme"] = &registry.Provider{ID: "trustme", IsActive: true}
	state.ProviderModels["trustme"] = map[string]*registry.ProviderModel{
		"m": {ProviderID: "trustme", ModelID: "m", PricingMode: "free", IsActive: true},
	}
	state.Accounts["acc1"] = &registry.Account{ID: "acc1", ProviderID: "trustme", IsActive: true}

	// Drive the shared trust manager to "trusted" (20/20 on the trust
	// dimension) the same way real successful traffic would.
	for i := 0; i < 12; i++ {
		globalTrustManager.RecordObservation("trustme", "m", "acc1", true, "")
	}
	if lvl := globalTrustManager.GetTrustLevel("trustme", "m", "acc1"); lvl != trust.TrustTrusted {
		t.Fatalf("test setup: expected trusted level, got %s", lvl)
	}

	h := &ChatHandler{}
	req, _ := http.NewRequest("GET", "/api/admin/explain-route?model=m&policy=balanced", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminExplainRoute(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rr.Code)
	}

	var res struct {
		Candidates []struct {
			ProviderID string `json:"ProviderID"`
			Score      struct {
				Dimensions map[string]float64 `json:"Dimensions"`
			} `json:"Score"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %s", len(res.Candidates), rr.Body.String())
	}
	if trustDim := res.Candidates[0].Score.Dimensions["trust"]; trustDim != 20.0 {
		t.Errorf("expected trust dimension to reflect the shared manager's trusted level (20), got %v — endpoint may be using a fresh trust.Manager again", trustDim)
	}
}

func TestHandleAdminSnapshotsAndRollback(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	registry.InitRegistry(database)
	state := registry.GetActiveState()
	state.Providers["p1"] = &registry.Provider{ID: "p1", IsActive: true}
	firstSnap, err := registry.CreateSnapshot(database, state, "first")
	if err != nil {
		t.Fatalf("failed to create first snapshot: %v", err)
	}
	if err := registry.ActivateSnapshot(database, firstSnap.Version); err != nil {
		t.Fatalf("failed to activate first snapshot: %v", err)
	}

	state.Providers["p2"] = &registry.Provider{ID: "p2", IsActive: true}
	secondSnap, err := registry.CreateSnapshot(database, state, "second")
	if err != nil {
		t.Fatalf("failed to create second snapshot: %v", err)
	}
	if err := registry.ActivateSnapshot(database, secondSnap.Version); err != nil {
		t.Fatalf("failed to activate second snapshot: %v", err)
	}

	// List should surface both snapshots (first is now last_known_good).
	req, _ := http.NewRequest("GET", "/admin/registry/snapshots", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminSnapshots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var listRes struct {
		Snapshots []registry.SnapshotMeta `json:"snapshots"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("failed to parse snapshots response: %v", err)
	}
	if len(listRes.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(listRes.Snapshots))
	}

	// Roll back to the explicit first version.
	rbReq, _ := http.NewRequest("POST", "/admin/registry/rollback?version="+firstSnap.Version, nil)
	rbRR := httptest.NewRecorder()
	h.HandleAdminRollback(rbRR, rbReq)
	if rbRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rbRR.Code, rbRR.Body.String())
	}
	rolledBack := registry.GetActiveState()
	if _, ok := rolledBack.Providers["p2"]; ok {
		t.Error("expected rollback to first snapshot to drop p2")
	}
	if _, ok := rolledBack.Providers["p1"]; !ok {
		t.Error("expected rollback to first snapshot to keep p1")
	}

	// Roll back with no version should fall back to last_known_good.
	rbReq2, _ := http.NewRequest("POST", "/admin/registry/rollback", nil)
	rbRR2 := httptest.NewRecorder()
	h.HandleAdminRollback(rbRR2, rbReq2)
	if rbRR2.Code != http.StatusOK {
		t.Fatalf("expected 200 for LKG rollback, got %d: %s", rbRR2.Code, rbRR2.Body.String())
	}
}
