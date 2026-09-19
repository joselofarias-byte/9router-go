package db

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunMigrations_EmptyDB(t *testing.T) {
	// Verify that migrations can run successfully on a completely fresh, empty database
	// This specifically tests that migration 0002 correctly handles the providerConnections triggers
	tempDB := "test_migrations_empty.db"
	defer os.Remove(tempDB)

	db, err := sql.Open("sqlite", tempDB)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations on empty DB: %v", err)
	}

	// Verify that the table was actually created
	_, err = db.Query("SELECT * FROM providerConnections LIMIT 1")
	if err != nil {
		t.Fatalf("expected providerConnections to exist after empty DB migration: %v", err)
	}
}

func TestRunMigrations_EmptyDBSchemaParity(t *testing.T) {
	tempDB := "test_migrations_parity.db"
	defer os.Remove(tempDB)

	db, err := sql.Open("sqlite", tempDB)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations on empty DB: %v", err)
	}

	// Verify required columns exist (lastUsedAt and consecutiveUseCount)
	rows, err := db.Query("PRAGMA table_info(providerConnections)")
	if err != nil {
		t.Fatalf("failed to get table info: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("failed to scan table info: %v", err)
		}
		columns[name] = true
	}

	requiredCols := []string{"lastUsedAt", "consecutiveUseCount"}
	for _, col := range requiredCols {
		if !columns[col] {
			t.Errorf("expected providerConnections to have column %s, but it was missing", col)
		}
	}

	// Verify required indexes exist
	iRows, err := db.Query("PRAGMA index_list(providerConnections)")
	if err != nil {
		t.Fatalf("failed to get index info: %v", err)
	}
	defer iRows.Close()

	indexes := make(map[string]bool)
	for iRows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := iRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("failed to scan index list: %v", err)
		}
		indexes[name] = true
	}

	requiredIndexes := []string{"idx_pc_provider", "idx_pc_provider_active", "idx_pc_priority"}
	for _, idx := range requiredIndexes {
		if !indexes[idx] {
			t.Errorf("expected providerConnections to have index %s, but it was missing", idx)
		}
	}
}

func TestRunMigrations(t *testing.T) {
	tempDB := "test_migrations.db"
	defer os.Remove(tempDB)

	db, err := sql.Open("sqlite", tempDB)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Use a unique version to avoid conflict with init() migrations
	RegisterMigration("9999", "create_test_table", func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY)")
		return err
	})

	if err := RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Verify table exists
	_, err = db.Query("SELECT * FROM test_table")
	if err != nil {
		t.Fatalf("expected test_table to exist: %v", err)
	}

	// Run again, should not fail or duplicate
	if err := RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations again: %v", err)
	}
}
