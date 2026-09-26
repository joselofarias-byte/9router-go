package chat

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/db"
)

func modelsIDs(t *testing.T, h *ChatHandler) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	h.HandleModels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestHandleModels_CustomOnlyWhenConnected(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatalf("delete connections: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM kv WHERE scope='customModels'`); err != nil {
		t.Fatalf("delete customs: %v", err)
	}

	// One active credentialed connection: codex/cx.
	cxData := `{"prefix":"cx","apiKey":"tok-codex-test"}`
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-cx-1', 'codex', 'oauth', 'Codex Account', 1, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, cxData); err != nil {
		t.Fatalf("seed codex: %v", err)
	}

	repo := db.NewRepo(database)
	// Custom for the connected provider: visible.
	if _, err := database.Exec(`INSERT INTO kv (scope, key, value) VALUES ('customModels', 'cx|my-custom|llm', ?)`,
		`{"providerAlias":"cx","id":"my-custom","type":"llm","name":"my-custom"}`); err != nil {
		t.Fatalf("seed custom cx: %v", err)
	}
	// Custom for a provider with no connection at all: hidden.
	if _, err := database.Exec(`INSERT INTO kv (scope, key, value) VALUES ('customModels', 'nvidia|ghost-model|llm', ?)`,
		`{"providerAlias":"nvidia","id":"ghost-model","type":"llm","name":"ghost-model"}`); err != nil {
		t.Fatalf("seed custom ghost: %v", err)
	}

	h := NewChatHandler(repo)
	ids := modelsIDs(t, h)
	joined := strings.Join(ids, "\n")
	if !strings.Contains(joined, "cx/my-custom") {
		t.Errorf("expected cx/my-custom listed, got:\n%s", joined)
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "nvidia/") || strings.HasPrefix(id, "nv/") {
			t.Errorf("ghost custom %q must not be listed without a connection", id)
		}
	}
}

func TestHandleModels_CustomHiddenWhenProviderInactive(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatalf("delete connections: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM kv WHERE scope='customModels'`); err != nil {
		t.Fatalf("delete customs: %v", err)
	}

	// Inactive (isActive=0) nvidia row: ghost stays hidden.
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-nv-off', 'nvidia', 'apikey', 'NV off', 1, 0, '{"apiKey":"sk-off"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`); err != nil {
		t.Fatalf("seed nvidia: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO kv (scope, key, value) VALUES ('customModels', 'nvidia|ghost-2|llm', ?)`,
		`{"providerAlias":"nvidia","id":"ghost-2","type":"llm","name":"ghost-2"}`); err != nil {
		t.Fatalf("seed custom: %v", err)
	}

	h := NewChatHandler(db.NewRepo(database))
	for _, id := range modelsIDs(t, h) {
		if strings.HasPrefix(id, "nvidia/") || strings.HasPrefix(id, "nv/") {
			t.Errorf("custom for inactive provider must not be listed, got %q", id)
		}
	}
}

func TestHandleModels_DisabledCustomExcluded(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatalf("delete connections: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM kv WHERE scope='customModels'`); err != nil {
		t.Fatalf("delete customs: %v", err)
	}

	cxData := `{"prefix":"cx","apiKey":"tok-codex-test"}`
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-cx-1', 'codex', 'oauth', 'Codex Account', 1, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, cxData); err != nil {
		t.Fatalf("seed codex: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO kv (scope, key, value) VALUES ('customModels', 'cx|gone|llm', ?)`,
		`{"providerAlias":"cx","id":"gone","type":"llm","name":"gone"}`); err != nil {
		t.Fatalf("seed custom: %v", err)
	}
	repo := db.NewRepo(database)
	if err := repo.SetKV("disabledModels", "cx", `["gone"]`); err != nil {
		t.Fatalf("seed disabled: %v", err)
	}

	h := NewChatHandler(repo)
	for _, id := range modelsIDs(t, h) {
		if id == "cx/gone" {
			t.Errorf("disabled custom cx/gone must be excluded")
		}
	}
}

func TestHandleModels_DisabledBuiltinExcluded(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatalf("delete connections: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM kv WHERE scope='customModels'`); err != nil {
		t.Fatalf("delete customs: %v", err)
	}

	cxData := `{"prefix":"cx","apiKey":"tok-codex-test"}`
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-cx-1', 'codex', 'oauth', 'Codex Account', 1, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`, cxData); err != nil {
		t.Fatalf("seed codex: %v", err)
	}
	repo := db.NewRepo(database)
	if err := repo.SetKV("disabledModels", "cx", `["gpt-6-astra"]`); err != nil {
		t.Fatalf("seed disabled: %v", err)
	}

	h := NewChatHandler(repo)
	ids := modelsIDs(t, h)
	joined := strings.Join(ids, "\n")
	if strings.Contains(joined, "cx/gpt-6-astra") {
		t.Errorf("disabled builtin cx/gpt-6-astra must be excluded, got:\n%s", joined)
	}
	if !strings.Contains(joined, "cx/gpt-5.6-sol") {
		t.Errorf("non-disabled cx/gpt-5.6-sol must stay, got:\n%s", joined)
	}
}
