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
	genMu         sync.RWMutex
	lastSyncGen   int
	hasSyncedOnce bool
)

// Injected Full Sync Query for Mock Testing Partial Failures
var queryFullAccounts = "SELECT id, provider, isActive, createdAt, updatedAt FROM providerConnections"

// InvalidateAccountSync forces the next SyncAccountsFromDB call to re-read
// providerConnections instead of trusting the generation fast path.
func InvalidateAccountSync() {
	genMu.Lock()
	hasSyncedOnce = false
	genMu.Unlock()
}

// RefreshAccountsAfterSnapshot drops the generation fast path and re-reads
// providerConnections. Discovery snapshots copy accounts at read time, so a
// later in-memory sync must win or a disconnected provider stays routable.
func RefreshAccountsAfterSnapshot(db *sql.DB) error {
	InvalidateAccountSync()
	return SyncAccountsFromDB(db)
}

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

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		currentGen, genKnown := readAccountGeneration(db)
		if genKnown {
			genMu.RLock()
			fresh := hasSyncedOnce && currentGen == lastSyncGen
			genMu.RUnlock()
			if fresh {
				return nil
			}
		}

		syncedAccounts, err := queryAccounts(db)
		if err != nil {
			return err
		}

		if genKnown {
			afterGen, afterKnown := readAccountGeneration(db)
			if afterKnown && afterGen != currentGen {
				// A connection changed while the scan was in flight. Discard
				// this snapshot so an older view cannot be published.
				lastErr = fmt.Errorf("accounts generation changed during sync")
				continue
			}
		}

		if publishAccounts(currentGen, genKnown, syncedAccounts) {
			log.Info("sync", "synchronized accounts from DB", "count", len(syncedAccounts))
			return nil
		}
		lastErr = fmt.Errorf("stale account snapshot discarded")
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("account sync did not converge")
	}
	return lastErr
}

func readAccountGeneration(db *sql.DB) (int, bool) {
	var currentGen int
	err := db.QueryRow("SELECT val FROM controlplane_meta WHERE key = 'accounts_generation'").Scan(&currentGen)
	if err == nil {
		return currentGen, true
	}
	if err != sql.ErrNoRows {
		log.Warn("sync", "failed to read accounts_generation meta", "error", err)
	}
	return 0, false
}

func queryAccounts(db *sql.DB) (map[string]*registry.Account, error) {
	rows, err := db.Query(queryFullAccounts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch provider connections: %w", err)
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
		return nil, fmt.Errorf("error iterating provider connections: %w", err)
	}
	return syncedAccounts, nil
}

// publishAccounts installs a scanned account map. A generation older than the
// one already published is ignored so a slower sync cannot restore a
// disconnected provider.
func publishAccounts(gen int, genKnown bool, accounts map[string]*registry.Account) bool {
	genMu.Lock()
	defer genMu.Unlock()
	if genKnown && hasSyncedOnce && gen < lastSyncGen {
		return false
	}
	registry.UpdateAccounts(accounts)
	if genKnown {
		if !hasSyncedOnce || gen >= lastSyncGen {
			lastSyncGen = gen
		}
		hasSyncedOnce = true
	}
	return true
}
