package db

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"9router/proxy/internal/log"
)

// Migration represents a single database schema migration.
type Migration struct {
	Version string
	Name    string
	Up      func(tx *sql.Tx) error
}

var migrations []Migration

// RegisterMigration adds a migration to the list of available migrations.
func RegisterMigration(version, name string, up func(tx *sql.Tx) error) {
	migrations = append(migrations, Migration{
		Version: version,
		Name:    name,
		Up:      up,
	})
}

// RunMigrations applies all pending migrations in order.
func RunMigrations(db *sql.DB) error {
	// Create the schema_migrations table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Get all applied migrations
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("failed to query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating over migrations: %w", err)
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// Apply pending migrations
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}

		log.Info("migrations", "applying migration", "version", m.Version, "name", m.Name)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %s: %w", m.Version, err)
		}

		if err := m.Up(tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to apply migration %s (%s): %w", m.Version, m.Name, err)
		}

		// Record the migration
		_, err = tx.Exec(
			"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
			m.Version, m.Name, time.Now().UTC().Format(time.RFC3339),
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", m.Version, err)
		}
	}

	return nil
}
