package sync

import (
	"database/sql"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/log"
)

// SyncAccountsFromDB reads providerConnections and safely syncs metadata into the RegistryState
func SyncAccountsFromDB(db *sql.DB) {
	if db == nil {
		return
	}

	state := registry.GetActiveState()
	if state == nil {
		log.Warn("sync", "cannot sync accounts: registry state not initialized")
		return
	}

	rows, err := db.Query("SELECT id, provider, isActive, createdAt, updatedAt FROM providerConnections")
	if err != nil {
		log.Warn("sync", "failed to fetch provider connections", "err", err)
		return
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
		log.Warn("sync", "error iterating provider connections", "err", err)
		return
	}

	// Update in-memory registry securely.
	registry.UpdateAccounts(syncedAccounts)
	log.Info("sync", "synchronized accounts from DB", "count", len(syncedAccounts))
}
