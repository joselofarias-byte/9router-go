//go:build capimux_apk && cgo

package db

import (
	"database/sql"

	"github.com/mattn/go-sqlite3"
)

// The APK uses SQLite through bionic. The pure-Go libc's legacy Linux
// filesystem syscalls are blocked by Android app seccomp on x86_64.
// The driver name and database file format remain compatible with the host.
func init() {
	sql.Register("sqlite", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000"} {
				if _, err := conn.Exec(pragma, nil); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
