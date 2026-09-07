package db

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

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
