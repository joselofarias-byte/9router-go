package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/controlplane/registry"
)

func TestHandleAdminRegistry_RedactsAuth(t *testing.T) {
	// Setup mock state
	registry.InitRegistry(nil)
	state := registry.GetActiveState()
	state.Accounts["test-acc"] = &registry.Account{
		ID:       "test-acc",
		AuthData: "super_secret_key",
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
	if acc.AuthData != "[REDACTED]" {
		t.Errorf("Expected AuthData to be redacted, got %s", acc.AuthData)
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
