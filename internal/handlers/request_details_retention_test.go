package handlers

import (
    "testing"
    "time"

    "9router/proxy/internal/db"
)

func TestRequestDetailsRetentionAndPurge(t *testing.T) {
    t.Setenv("CAPIMUX_REQUEST_DETAILS_RETENTION_DAYS", "7")
    database, cleanup := setupTestDB(t)
    defer cleanup()
    repo := db.NewRepo(database)

    expired := time.Now().UTC().Add(-10 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
    if _, err := database.Exec(`INSERT INTO requestDetails (id, timestamp, provider, model, connectionId, status, data)
       VALUES ('grok004-expired', ?, 'p', 'm', 'c', 'success', '{"secret":"old-canary"}')`, expired); err != nil {
        t.Fatalf("insert expired row: %v", err)
    }
    if err := repo.InsertRequestDetail("grok004-recent", "p", "m", "c", "success", `{"id":"grok004-recent"}`); err != nil {
        t.Fatalf("insert recent row: %v", err)
    }

    values, count, err := repo.GetRequestDetailsPaged(10, 0)
    if err != nil {
        t.Fatalf("query details: %v", err)
    }
    if count != 1 || len(values) != 1 || values[0] != `{"id":"grok004-recent"}` {
        t.Fatalf("expired content visible: count=%d values=%v", count, values)
    }
    removed, err := repo.PurgeAllRequestDetails()
    if err != nil || removed != 1 {
        t.Fatalf("purge count=%d err=%v", removed, err)
    }
    _, count, err = repo.GetRequestDetailsPaged(10, 0)
    if err != nil || count != 0 {
        t.Fatalf("purge incomplete: count=%d err=%v", count, err)
    }
}
