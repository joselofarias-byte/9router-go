package db

import (
	"database/sql"
)

func init() {
	RegisterMigration("0001", "create_registry_snapshots", func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS registry_snapshots (
				version TEXT PRIMARY KEY,
				created_at TEXT NOT NULL,
				reason TEXT NOT NULL,
				checksum TEXT NOT NULL,
				status TEXT NOT NULL, -- 'candidate', 'active', 'last_known_good'
				payload TEXT NOT NULL
			);
		`)
		return err
	})

	RegisterMigration("0002", "create_controlplane_meta_and_triggers", func(tx *sql.Tx) error {
		// Ensure providerConnections exists before creating triggers,
		// so empty-DB bootstraps do not fail during this migration.
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS providerConnections (
				id TEXT PRIMARY KEY,
				provider TEXT NOT NULL,
				authType TEXT NOT NULL,
				name TEXT,
				email TEXT,
				priority INTEGER,
				isActive INTEGER DEFAULT 1,
				data TEXT NOT NULL,
				lastUsedAt TEXT,
				consecutiveUseCount INTEGER DEFAULT 0,
				createdAt TEXT NOT NULL,
				updatedAt TEXT NOT NULL
			);
		`); err != nil {
			return err
		}

		// Create parity indexes
		indexQueries := []string{
			`CREATE INDEX IF NOT EXISTS idx_pc_provider ON providerConnections(provider);`,
			`CREATE INDEX IF NOT EXISTS idx_pc_provider_active ON providerConnections(provider, isActive);`,
			`CREATE INDEX IF NOT EXISTS idx_pc_priority ON providerConnections(provider, priority);`,
		}
		for _, q := range indexQueries {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}

		// Create the metadata table for tracking monotonic generation counts
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS controlplane_meta (
				key TEXT PRIMARY KEY,
				val INTEGER NOT NULL
			);
		`); err != nil {
			return err
		}

		// Initialize the generation counter if it doesn't exist
		if _, err := tx.Exec(`
			INSERT INTO controlplane_meta (key, val)
			VALUES ('accounts_generation', 1)
			ON CONFLICT(key) DO NOTHING;
		`); err != nil {
			return err
		}

		// Create triggers to atomically increment the generation on any mutation to providerConnections
		triggerQueries := []string{
			`CREATE TRIGGER IF NOT EXISTS trg_providerConnections_after_insert
			 AFTER INSERT ON providerConnections
			 BEGIN
				 UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation';
			 END;`,
			`CREATE TRIGGER IF NOT EXISTS trg_providerConnections_after_update
			 AFTER UPDATE ON providerConnections
			 BEGIN
				 UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation';
			 END;`,
			`CREATE TRIGGER IF NOT EXISTS trg_providerConnections_after_delete
			 AFTER DELETE ON providerConnections
			 BEGIN
				 UPDATE controlplane_meta SET val = val + 1 WHERE key = 'accounts_generation';
			 END;`,
		}

		for _, query := range triggerQueries {
			if _, err := tx.Exec(query); err != nil {
				return err
			}
		}

		return nil
	})

	// Add other registry tables if they were to be persisted.
	// We'll keep it primarily in the snapshot payload to satisfy requirements and simplify atomicity for now,
	// but can expand to individual tables if needed later.
}
