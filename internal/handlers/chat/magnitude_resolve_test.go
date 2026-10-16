package chat

import (
    "testing"

    "9router/proxy/internal/db"
)

// No Magnitude daemon is required: this verifies that the normal model
// resolver recognizes the new provider namespace before forwarding.
func TestResolveModel_MagnitudeNamespace(t *testing.T) {
    database, cleanup := setupChatTestDB(t)
    defer cleanup()

    handler := NewChatHandler(db.NewRepo(database))
    info, err := handler.resolveModel("magnitude/test-model")
    if err != nil {
        t.Fatalf("magnitude model resolution failed: %v", err)
    }
    if info.Provider != "magnitude" || info.Model != "test-model" {
        t.Fatalf("unexpected model resolution: provider=%q model=%q", info.Provider, info.Model)
    }
}
