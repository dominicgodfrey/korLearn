package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
)

// The in-memory tests never exercise the DSN, where a Windows path
// (C:\Users\...\korlearn.db) has to survive escaping intact.
func TestOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "korlearn.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	if err := db.LoadLessons(context.Background(), []seed.Lesson{lesson(1, "가")}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Reopening must find the same database, not create a second one.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	var n int
	if err := db2.QueryRow(`SELECT count(*) FROM vocab`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("vocab count after reopen = %d, want 1", n)
	}
}

// The DSN pragmas are the only thing enforcing referential integrity; SQLite
// ignores foreign keys by default and would accept an orphaned attempt.
func TestOpenFileEnforcesForeignKeys(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "korlearn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Fatalf("foreign_keys = %d, want 1", on)
	}

	if _, err := db.Exec(`
		INSERT INTO attempts (session_id, item_type, item_id, stage, correct, original_correct)
		VALUES (999, 'vocab', 1, 'typed', 1, 1)`); err == nil {
		t.Error("inserted an attempt against a nonexistent session, want a foreign key error")
	}

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}
