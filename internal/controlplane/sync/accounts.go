package sync

import (
	"database/sql"
	"fmt"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/log"
)

import "sync"

var (
	genMu           sync.RWMutex
	lastSyncGenTime string
	lastSyncGenCnt  int
)

// Injected Full Sync Query for Mock Testing Partial Failures
var queryFullAccounts = "SELECT id, provider, isActive, createdAt, updatedAt FROM providerConnections"

// SyncAccountsFromDB reads providerConnections and safely syncs metadata into the RegistryState
// using a fast-path generation check to avoid expensive querying when no mutations occurred.
func SyncAccountsFromDB(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	state := registry.GetActiveState()
	if state == nil {
		return fmt.Errorf("cannot sync accounts: registry state not initialized")
	}

	// 1. Generation-based fast-path check
	// Check the latest updatedAt and the absolute count of rows.
	var currentGenTime sql.NullString
	var currentGenCnt int
	err := db.QueryRow("SELECT MAX(updatedAt), COUNT(*) FROM providerConnections").Scan(&currentGenTime, &currentGenCnt)

	genTimeStr := ""
	if err == nil {
		if currentGenTime.Valid {
			genTimeStr = currentGenTime.String
		}

		genMu.RLock()
		isEqual := genTimeStr == lastSyncGenTime && currentGenCnt == lastSyncGenCnt
		genMu.RUnlock()

		if isEqual {
			// No changes detected in the DB, safe to abort full sync
			return nil
		}
	}

	// 2. Full synchronization
	rows, err := db.Query(queryFullAccounts)
	if err != nil {
		return fmt.Errorf("failed to fetch provider connections: %w", err)
	}
	defer rows.Close()

	syncedAccounts := make(map[string]*registry.Account)

	for rows.Next() {
		var id, provider, createdAt, updatedAt string
		var isActive int
		if err := rows.Scan(&id, &provider, &isActive, &createdAt, &updatedAt); err == nil {
			acc := &registry.Account{
				ID:         id,
				ProviderID: provider,
				IsActive:   isActive == 1,
			}
			if t, e := time.Parse(time.RFC3339, createdAt); e == nil {
				acc.CreatedAt = t
			}
			if t, e := time.Parse(time.RFC3339, updatedAt); e == nil {
				acc.UpdatedAt = t
			}
			syncedAccounts[id] = acc
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating provider connections: %w", err)
	}

	// Update in-memory registry securely.
	registry.UpdateAccounts(syncedAccounts)
	log.Info("sync", "synchronized accounts from DB", "count", len(syncedAccounts))

	// Publish the new generation only after full scan succeeds
	genMu.Lock()
	lastSyncGenTime = genTimeStr
	lastSyncGenCnt = currentGenCnt
	genMu.Unlock()

	return nil
}
