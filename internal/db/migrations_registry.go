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

	// Add other registry tables if they were to be persisted.
	// We'll keep it primarily in the snapshot payload to satisfy requirements and simplify atomicity for now,
	// but can expand to individual tables if needed later.
}
