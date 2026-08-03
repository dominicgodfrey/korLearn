// Package store owns the SQLite database: schema migrations, and loading
// parsed seed content into it.
//
// The driver is modernc.org/sqlite, a pure-Go translation of SQLite. It is
// slower than the cgo binding but needs no C toolchain, which on Windows is the
// difference between "go build" working and an afternoon of gcc archaeology.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"slices"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps the connection pool. It is a struct rather than a bare *sql.DB so
// that query methods have somewhere to live as the app grows.
type DB struct {
	*sql.DB
}

// Open opens (creating if absent) the database at path and applies any pending
// migrations. Pass ":memory:" for tests.
func Open(path string) (*DB, error) {
	dsn := path
	if path != ":memory:" {
		// Enforce foreign keys and use WAL so a reader never blocks the writer.
		// modernc.org/sqlite takes pragmas as DSN query parameters.
		dsn = "file:" + url.PathEscape(path) +
			"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	// A single connection. This is a single-user localhost app, so serializing
	// access costs nothing measurable and removes every "database is locked"
	// failure mode, including the one where an in-memory DB silently becomes a
	// different empty database on each new connection.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("connect %s: %w", path, err)
	}

	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// migrate applies every embedded migration that has not run yet, in filename
// order, each in its own transaction. No migration library: the files are
// append-only and never edited once committed, which is the whole protocol.
func (db *DB) migrate() error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)

	for _, name := range names {
		var applied int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}
