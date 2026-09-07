package registry

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"9router/proxy/internal/log"
)

var (
	activeState    *RegistryState
	lastKnownGood  *RegistryState
	stateMu        sync.RWMutex
)

// InitRegistry initialize registry from active snapshot if exists.
func InitRegistry(db *sql.DB) error {
	if db != nil {
		snap, err := GetActiveSnapshot(db)
		if err != nil && err != sql.ErrNoRows {
			return err
		}

		isValid := false
		if snap != nil {
			if err := ValidateChecksum(snap.Checksum, snap.Payload); err != nil {
				log.Warn("registry", "active snapshot checksum invalid, attempting last_known_good recovery", "err", err)
			} else {
				state, err := FromJSON(snap.Payload)
				if err != nil {
					log.Warn("registry", "failed to parse active snapshot, attempting last_known_good recovery", "err", err)
				} else {
					stateMu.Lock()
					activeState = state
					lastKnownGood = state
					stateMu.Unlock()
					log.Info("registry", "Loaded active snapshot", "version", snap.Version)
					isValid = true
				}
			}
		}

		// Attempt LKG recovery if active snapshot was invalid or missing
		if !isValid {
			// Flag the corrupt active snapshot as broken so it isn't erroneously cycled to LKG later
			if snap != nil && snap.Version != "" {
				_, _ = db.Exec("UPDATE registry_snapshots SET status = 'corrupt' WHERE version = ?", snap.Version)
			}

			// Iterate through LKGs descending to find the first valid one
			// We cannot insert while holding the rows cursor open due to SQLite locks
			var validLKG *Snapshot
			rows, err := db.Query("SELECT version, created_at, reason, checksum, status, payload FROM registry_snapshots WHERE status = 'last_known_good' ORDER BY created_at DESC")
			if err == nil {
				for rows.Next() {
					var lkg Snapshot
					var createdAtStr string
					if err := rows.Scan(&lkg.Version, &createdAtStr, &lkg.Reason, &lkg.Checksum, &lkg.Status, &lkg.Payload); err == nil {
						if err := ValidateChecksum(lkg.Checksum, lkg.Payload); err == nil {
							if _, err := FromJSON(lkg.Payload); err == nil {
								validLKG = &lkg
								break
							}
						}
					}
				}
				rows.Close()
			}

			if validLKG != nil {
				if state, err := FromJSON(validLKG.Payload); err == nil {
					// Persist the recovered snapshot as the new active snapshot
					newVersion := uuid.New().String()
					nowStr := time.Now().UTC().Format(time.RFC3339)
					reason := fmt.Sprintf("recovery_from_%s", validLKG.Version)

					_, execErr := db.Exec(
						`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload)
						VALUES (?, ?, ?, ?, ?, ?)`,
						newVersion, nowStr, reason, validLKG.Checksum, "active", validLKG.Payload,
					)

					if execErr == nil {
						stateMu.Lock()
						activeState = state
						lastKnownGood = state
						stateMu.Unlock()
						log.Info("registry", "Recovered from last_known_good snapshot and persisted as active", "lkg_version", validLKG.Version, "new_active_version", newVersion)
						return nil
					} else {
						log.Warn("registry", "Failed to persist recovered active snapshot", "err", execErr)
					}
				}
			}
			log.Warn("registry", "no valid snapshot or last_known_good available, falling back to empty state")
		} else {
			return nil
		}
	}

	// Initialize empty state
	stateMu.Lock()
	activeState = &RegistryState{
		Providers:      make(map[string]*Provider),
		Models:         make(map[string]*Model),
		ProviderModels: make(map[string]map[string]*ProviderModel),
		Accounts:       make(map[string]*Account),
	}
	stateMu.Unlock()
	log.Info("registry", "Initialized empty registry state")

	return nil
}

// GetLastKnownGoodSnapshot returns the most recent 'last_known_good' snapshot.
func GetLastKnownGoodSnapshot(db *sql.DB) (*Snapshot, error) {
	if db == nil {
		return nil, sql.ErrNoRows
	}

	var snap Snapshot
	var createdAtStr string
	err := db.QueryRow(
		"SELECT version, created_at, reason, checksum, status, payload FROM registry_snapshots WHERE status = 'last_known_good' ORDER BY created_at DESC LIMIT 1",
	).Scan(&snap.Version, &createdAtStr, &snap.Reason, &snap.Checksum, &snap.Status, &snap.Payload)

	if err != nil {
		return nil, err
	}

	t, err := time.Parse(time.RFC3339, createdAtStr)
	if err == nil {
		snap.CreatedAt = t
	}

	return &snap, nil
}

// GetActiveState returns the current active registry state.
func GetActiveState() *RegistryState {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return activeState
}

// GenerateChecksum creates a SHA256 checksum for a given payload.
func GenerateChecksum(payload string) string {
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}

// CreateSnapshot creates a new snapshot from a RegistryState and saves it.
func CreateSnapshot(db *sql.DB, state *RegistryState, reason string) (*Snapshot, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}

	payload, err := state.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize state: %w", err)
	}

	snap := &Snapshot{
		Version:   uuid.New().String(),
		CreatedAt: time.Now().UTC(),
		Reason:    reason,
		Checksum:  GenerateChecksum(payload),
		Status:    "candidate",
		Payload:   payload,
	}

	_, err = db.Exec(
		`INSERT INTO registry_snapshots (version, created_at, reason, checksum, status, payload)
		VALUES (?, ?, ?, ?, ?, ?)`,
		snap.Version, snap.CreatedAt.Format(time.RFC3339), snap.Reason, snap.Checksum, snap.Status, snap.Payload,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert snapshot: %w", err)
	}

	return snap, nil
}

// ActivateSnapshot marks a snapshot as "active" and updates the in-memory state.
func ActivateSnapshot(db *sql.DB, version string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	var payload string
	var checksum string
	err = tx.QueryRow("SELECT payload, checksum FROM registry_snapshots WHERE version = ?", version).Scan(&payload, &checksum)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load snapshot: %w", err)
	}

	if err := ValidateChecksum(checksum, payload); err != nil {
		tx.Rollback()
		return fmt.Errorf("snapshot validation failed before activation: %w", err)
	}

	newState, err := FromJSON(payload)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to parse snapshot payload: %w", err)
	}

	// Move current active to last_known_good
	_, err = tx.Exec("UPDATE registry_snapshots SET status = 'last_known_good' WHERE status = 'active'")
	if err != nil {
		tx.Rollback()
		return err
	}

	// Set new active
	_, err = tx.Exec("UPDATE registry_snapshots SET status = 'active' WHERE version = ?", version)
	if err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	stateMu.Lock()
	if activeState != nil {
		lastKnownGood = activeState
	}
	activeState = newState
	stateMu.Unlock()

	log.Info("registry", "Activated snapshot", "version", version)
	return nil
}

// UpdateAccounts safely replaces the accounts map in the active state
// via a Copy-on-Write operation, preventing data races during concurrent read/iteration.
func UpdateAccounts(accounts map[string]*Account) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if activeState != nil {
		newState := &RegistryState{
			Providers:      activeState.Providers,
			Models:         activeState.Models,
			ProviderModels: activeState.ProviderModels,
			Accounts:       accounts,
		}
		activeState = newState
		if lastKnownGood == nil {
			lastKnownGood = newState
		}
	}
}

// GetActiveSnapshot returns the currently active snapshot from the database.
func GetActiveSnapshot(db *sql.DB) (*Snapshot, error) {
	if db == nil {
		return nil, sql.ErrNoRows
	}

	var snap Snapshot
	var createdAtStr string
	err := db.QueryRow(
		"SELECT version, created_at, reason, checksum, status, payload FROM registry_snapshots WHERE status = 'active' ORDER BY created_at DESC LIMIT 1",
	).Scan(&snap.Version, &createdAtStr, &snap.Reason, &snap.Checksum, &snap.Status, &snap.Payload)

	if err != nil {
		return nil, err
	}

	t, err := time.Parse(time.RFC3339, createdAtStr)
	if err == nil {
		snap.CreatedAt = t
	}

	return &snap, nil
}
