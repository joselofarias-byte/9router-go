package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/db"
)

func deleteNodeRequest(t *testing.T, repo *db.Repo, id string) *httptest.ResponseRecorder {
	t.Helper()
	router := setupTestRouter(repo)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/provider-nodes/"+id, nil))
	return rec
}

func createNodeDeleteFixture(t *testing.T, repo *db.Repo, id, provider string) {
	t.Helper()
	if err := repo.CreateProviderConnection(id, provider, "oauth", "synthetic-"+id,
		"{\"accessToken\":\"test-access-never-real\",\"refreshToken\":\"test-refresh-never-real\"}"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteProviderNode_CannotPurgeBuiltInGrokAccounts(t *testing.T) {
	repo, cleanup := setupNodeTestDB(t)
	defer cleanup()
	for i := 0; i < 10; i++ {
		createNodeDeleteFixture(t, repo, fmt.Sprintf("grok-fixture-%02d", i), "grok-cli")
	}

	rec := deleteNodeRequest(t, repo, "grok-cli")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("deleting built-in grok-cli node: HTTP %d, body %s", rec.Code, rec.Body.String())
	}
	conns, err := repo.GetProviderConnections("grok-cli", false)
	if err != nil || len(conns) != 10 {
		t.Fatalf("built-in Grok accounts must remain: got %d, err %v", len(conns), err)
	}

	// A malformed import could supply a providerNodes row with a colliding
	// built-in ID. A real node with that unsafe ID is also never deletable.
	if _, err := repo.CreateProviderNode("grok-cli", "openai-compatible", "collision", "{}"); err != nil {
		t.Fatal(err)
	}
	rec = deleteNodeRequest(t, repo, "grok-cli")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("colliding node must be rejected: HTTP %d", rec.Code)
	}
	conns, err = repo.GetProviderConnections("grok-cli", false)
	if err != nil || len(conns) != 10 {
		t.Fatalf("Grok accounts lost after colliding node: n=%d, err=%v", len(conns), err)
	}
	if n, _, err := repo.GetProviderNodeByID("grok-cli"); err != nil || n == nil {
		t.Fatalf("colliding node should not be changed: n=%v err=%v", n, err)
	}
}

func TestDeleteProviderNode_MissingCustomNodeKeepsOrphanConnections(t *testing.T) {
	repo, cleanup := setupNodeTestDB(t)
	defer cleanup()

	const id = "openai-compatible-chat-orphan"
	createNodeDeleteFixture(t, repo, "orphan-one", id)
	rec := deleteNodeRequest(t, repo, id)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nonexistent node must return 404, got %d: %s", rec.Code, rec.Body.String())
	}
	conns, err := repo.GetProviderConnections(id, false)
	if err != nil || len(conns) != 1 {
		t.Fatalf("orphan connection must survive: n=%d err=%v", len(conns), err)
	}
}

func TestDeleteProviderNode_ExistingCustomNodeRemovesOnlyItsConnections(t *testing.T) {
	for _, tt := range []struct {
		nodeID string
		kind   string
	}{
		{"openai-compatible-chat-node-a", "openai-compatible"},
		{"anthropic-compatible-node-b", "anthropic-compatible"},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			repo, cleanup := setupNodeTestDB(t)
			defer cleanup()

			if _, err := repo.CreateProviderNode(tt.nodeID, tt.kind, "Custom", "{\"prefix\":\"custom\"}"); err != nil {
				t.Fatal(err)
			}
			createNodeDeleteFixture(t, repo, "a1", tt.nodeID)
			createNodeDeleteFixture(t, repo, "a2", tt.nodeID)
			createNodeDeleteFixture(t, repo, "grok-preserved", "grok-cli")

			rec := deleteNodeRequest(t, repo, tt.nodeID)
			if rec.Code != http.StatusOK {
				t.Fatalf("deleting legitimate node: HTTP %d body %s", rec.Code, rec.Body.String())
			}
			conns, err := repo.GetProviderConnections(tt.nodeID, false)
			if err != nil || len(conns) != 0 {
				t.Fatalf("attached connections remain: n=%d, err=%v", len(conns), err)
			}
			grok, err := repo.GetProviderConnections("grok-cli", false)
			if err != nil || len(grok) != 1 {
				t.Fatalf("unrelated Grok connection removed: n=%d err=%v", len(grok), err)
			}
			node, _, err := repo.GetProviderNodeByID(tt.nodeID)
			if err != nil || node != nil {
				t.Fatalf("deleted node still exists: node=%v err=%v", node, err)
			}
		})
	}
}

func TestDeleteProviderNode_RollbackOnConnectionDeleteFailure(t *testing.T) {
	repo, cleanup := setupNodeTestDB(t)
	defer cleanup()
	const id = "openai-compatible-chat-rollback"
	if _, err := repo.CreateProviderNode(id, "openai-compatible", "Rollback", "{}"); err != nil {
		t.Fatal(err)
	}
	createNodeDeleteFixture(t, repo, "to-rollback", id)
	_, err := repo.RawDB().Exec("CREATE TRIGGER fail_deletion BEFORE DELETE ON providerConnections "+
		"WHEN OLD.provider = 'openai-compatible-chat-rollback' "+
		"BEGIN SELECT RAISE(ABORT, 'synthetic stop'); END")
	if err != nil {
		t.Fatal(err)
	}

	rec := deleteNodeRequest(t, repo, id)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed delete should return HTTP 500, got %d", rec.Code)
	}
	node, _, err := repo.GetProviderNodeByID(id)
	if err != nil || node == nil {
		t.Fatalf("failed transaction must keep node: node=%v err=%v", node, err)
	}
	conns, err := repo.GetProviderConnections(id, false)
	if err != nil || len(conns) != 1 {
		t.Fatalf("failed transaction must keep connection: n=%d err=%v", len(conns), err)
	}
}

func TestDeleteProviderNode_TypeMismatchRejected(t *testing.T) {
	repo, cleanup := setupNodeTestDB(t)
	defer cleanup()
	const id = "openai-compatible-chat-type-mismatch"
	if _, err := repo.CreateProviderNode(id, "anthropic-compatible", "Mismatch", "{}"); err != nil {
		t.Fatal(err)
	}
	createNodeDeleteFixture(t, repo, "mismatch-account", id)
	rec := deleteNodeRequest(t, repo, id)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched custom node should return HTTP 400, got %d", rec.Code)
	}
	conns, err := repo.GetProviderConnections(id, false)
	if err != nil || len(conns) != 1 {
		t.Fatalf("node type mismatch cannot remove connection: n=%d err=%v", len(conns), err)
	}
}
